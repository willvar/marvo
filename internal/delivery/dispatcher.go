package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"marvo/internal/connectors"
	"marvo/internal/control"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

const (
	deliveryLease       = time.Minute
	stateOpenRetryDelay = time.Minute
)

var retryDelays = [...]time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	time.Hour,
}

type Dispatcher struct {
	control      *control.DB
	layout       *userspace.Layout
	masterSecret string
	publicURL    string
	registry     *connectors.Registry
	workerLimit  int

	mu       sync.Mutex
	pending  map[string]struct{}
	notify   chan struct{}
	done     chan workerResult
	started  atomic.Bool
	scanning atomic.Bool
	wg       sync.WaitGroup
}

type workerResult struct {
	userID  string
	nextDue *time.Time
	err     error
}

func NewDispatcher(controlDB *control.DB, layout *userspace.Layout, masterSecret, publicURL string, registry *connectors.Registry) *Dispatcher {
	workerLimit := runtime.GOMAXPROCS(0)
	if workerLimit < 2 {
		workerLimit = 2
	}
	if workerLimit > 8 {
		workerLimit = 8
	}
	return &Dispatcher{
		control: controlDB, layout: layout, masterSecret: masterSecret,
		publicURL: strings.TrimRight(publicURL, "/"), registry: registry, workerLimit: workerLimit,
		pending: make(map[string]struct{}), notify: make(chan struct{}, 1), done: make(chan workerResult, workerLimit),
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	if d == nil || d.control == nil || d.layout == nil || d.registry == nil || !d.started.CompareAndSwap(false, true) {
		return
	}
	d.wg.Add(1)
	go d.run(ctx)
	d.Resync(ctx)
}

func (d *Dispatcher) Wait() {
	if d != nil {
		d.wg.Wait()
	}
}

func (d *Dispatcher) Wake(userID string) {
	if d == nil || !control.ValidateUserID(userID) {
		return
	}
	d.mu.Lock()
	d.pending[userID] = struct{}{}
	d.mu.Unlock()
	select {
	case d.notify <- struct{}{}:
	default:
	}
}

// Resync is deliberately event-driven rather than periodic. It repairs the
// only two loss windows: process restart and Runtime event-stream reconnect.
func (d *Dispatcher) Resync(ctx context.Context) {
	if d == nil || d.control == nil || !d.scanning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer d.scanning.Store(false)
		users, err := d.control.ListUsers(ctx)
		if err != nil {
			slog.Error("connector delivery: resync users failed", "error", err)
			return
		}
		for _, user := range users {
			if user.Status == control.UserStatusActive {
				d.Wake(user.ID)
			}
		}
	}()
}

func (d *Dispatcher) run(ctx context.Context) {
	defer d.wg.Done()
	due := make(map[string]time.Time)
	running := make(map[string]bool)
	var timer *time.Timer
	var timerChannel <-chan time.Time

	resetTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		var earliest time.Time
		for userID, when := range due {
			if running[userID] {
				continue
			}
			if earliest.IsZero() || when.Before(earliest) {
				earliest = when
			}
		}
		if earliest.IsZero() {
			timerChannel = nil
			return
		}
		delay := time.Until(earliest)
		if delay < 0 {
			delay = 0
		}
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerChannel = timer.C
	}

	drainWakeups := func() {
		d.mu.Lock()
		for userID := range d.pending {
			due[userID] = time.Now()
			delete(d.pending, userID)
		}
		d.mu.Unlock()
	}

	dispatch := func() {
		capacity := d.workerLimit - len(running)
		if capacity <= 0 {
			return
		}
		now := time.Now()
		for userID, when := range due {
			if capacity == 0 {
				break
			}
			if running[userID] || when.After(now) {
				continue
			}
			delete(due, userID)
			running[userID] = true
			capacity--
			d.wg.Add(1)
			go func(userID string) {
				defer d.wg.Done()
				result := d.processUser(ctx, userID)
				select {
				case d.done <- result:
				case <-ctx.Done():
				}
			}(userID)
		}
	}

	for {
		drainWakeups()
		dispatch()
		resetTimer()
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-d.notify:
		case result := <-d.done:
			delete(running, result.userID)
			if result.err != nil && !errors.Is(result.err, context.Canceled) {
				slog.Error("connector delivery: process user failed", "user_id", result.userID, "error", result.err)
			}
			if result.nextDue != nil {
				if existing, ok := due[result.userID]; !ok || result.nextDue.Before(existing) {
					due[result.userID] = *result.nextDue
				}
			}
		case <-timerChannel:
		}
	}
}

func (d *Dispatcher) processUser(ctx context.Context, userID string) workerResult {
	result := workerResult{userID: userID}
	user, err := d.control.GetUser(ctx, userID)
	if err != nil || user.Status != control.UserStatusActive {
		if err != nil && !errors.Is(err, control.ErrUserNotFound) {
			result.err = err
		}
		return result
	}
	connectorStore, closeState, err := d.openStore(userID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			next := time.Now().Add(stateOpenRetryDelay)
			result.nextDue = &next
			result.err = err
		}
		return result
	}
	defer closeState()

	for ctx.Err() == nil {
		now := time.Now().UTC()
		delivery, err := connectorStore.ClaimDue(now, deliveryLease)
		if err != nil {
			result.err = err
			break
		}
		if delivery == nil {
			break
		}
		message := d.message(userID, *delivery)
		sendErr := d.registry.Send(ctx, delivery.Connector.ProviderID, delivery.Connector.Config, message)
		if sendErr == nil {
			if err := connectorStore.MarkSent(delivery.ID, time.Now().UTC()); err != nil {
				result.err = err
				break
			}
			continue
		}
		final := connectors.IsPermanent(sendErr) || delivery.Attempt > len(retryDelays)
		delay := time.Duration(0)
		if !final {
			delay = retryDelays[delivery.Attempt-1]
			if retryAfter := connectors.RetryAfter(sendErr); retryAfter > delay {
				delay = retryAfter
			}
		}
		failedAt := time.Now().UTC()
		errorMessage := d.registry.RedactError(delivery.Connector.ProviderID, delivery.Connector.Config, sendErr.Error())
		if err := connectorStore.MarkFailed(delivery.ID, failedAt, failedAt.Add(delay), final, errorMessage); err != nil {
			result.err = err
			break
		}
	}
	if result.err == nil && ctx.Err() != nil {
		result.err = ctx.Err()
	}
	if next, err := connectorStore.NextDue(); err != nil && result.err == nil {
		result.err = err
	} else {
		result.nextDue = next
	}
	return result
}

func (d *Dispatcher) openStore(userID string) (*store.ConnectorStore, func(), error) {
	paths, err := d.layout.UserPaths(userID)
	if err != nil {
		return nil, func() {}, err
	}
	databasePath := filepath.Join(paths.Workspace, ".marvo", "state.sqlite")
	info, err := os.Lstat(databasePath)
	if err != nil {
		return nil, func() {}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, func() {}, fmt.Errorf("user state database is not a regular file")
	}
	state, err := store.OpenStateDB(paths.Workspace)
	if err != nil {
		return nil, func() {}, err
	}
	connectorStore, err := store.NewConnectorStore(state, userID, d.masterSecret)
	if err != nil {
		_ = state.Close()
		return nil, func() {}, err
	}
	return connectorStore, func() { _ = state.Close() }, nil
}

func (d *Dispatcher) message(userID string, delivery store.ClaimedDelivery) connectors.Message {
	content := delivery.Activity.Content
	if delivery.Activity.Kind == store.ActivityKindChoice && len(delivery.Activity.Choices) > 0 {
		content += "\n\n可选：\n- " + strings.Join(delivery.Activity.Choices, "\n- ")
	}
	activityURL := fmt.Sprintf("%s/user/%s/activity?activity=%s", d.publicURL, userID, delivery.Activity.ID)
	return connectors.Message{
		DeliveryID: delivery.ID, ActivityID: delivery.Activity.ID, Kind: delivery.Activity.Kind,
		Title: delivery.Activity.Title, Content: content, URL: activityURL, CreatedAt: delivery.Activity.CreatedAt,
	}
}
