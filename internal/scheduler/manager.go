package scheduler

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
	"unicode/utf8"

	"marvo/internal/control"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

const (
	defaultRunTimeout = time.Hour
	defaultRunLease   = 2 * time.Minute
	stateOpenRetry    = time.Minute
)

var (
	ErrRunStopped   = errors.New("automatic task run stopped")
	ErrUserStopped  = errors.New("automatic task user disabled")
	ErrRunTimeout   = errors.New("automatic task run timed out")
	ErrRunLeaseLost = errors.New("automatic task run lease lost")
)

var retryDelays = [...]time.Duration{
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
}

type ExecutionResult struct {
	SessionID         string
	MessageID         string
	FinalText         string
	ActivityPublished bool
	Retryable         bool
	Error             error
}

type Recorder interface {
	SetSession(context.Context, string) error
	SetRequestMessage(context.Context, string) error
	SetResponseMessage(context.Context, string) error
}

type Executor interface {
	Execute(context.Context, string, store.ClaimedScheduleRun, Recorder) ExecutionResult
}

type Manager struct {
	control  *control.DB
	layout   *userspace.Layout
	executor Executor

	workerLimit int
	runTimeout  time.Duration
	lease       time.Duration
	now         func() time.Time

	started atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	mu      sync.Mutex
	pending map[string]struct{}
	active  map[string]*activeRun
	notify  chan struct{}
	claims  chan claimResult
	done    chan runResult

	changeMu   sync.RWMutex
	onChanged  func(string)
	onActivity func(string)
}

type activeRun struct {
	userID     string
	scheduleID string
	runID      string
	cancel     context.CancelCauseFunc
}

type claimResult struct {
	userID string
	state  *store.StateDB
	store  *store.ScheduleStore
	claim  *store.ClaimedScheduleRun
	next   *time.Time
	err    error
}

type runResult struct {
	userID string
	next   *time.Time
	err    error
}

func NewManager(controlDB *control.DB, layout *userspace.Layout, executor Executor) *Manager {
	workerLimit := runtime.GOMAXPROCS(0)
	if workerLimit < 2 {
		workerLimit = 2
	}
	if workerLimit > 8 {
		workerLimit = 8
	}
	return &Manager{
		control: controlDB, layout: layout, executor: executor,
		workerLimit: workerLimit, runTimeout: defaultRunTimeout, lease: defaultRunLease, now: time.Now,
		pending: make(map[string]struct{}), active: make(map[string]*activeRun), notify: make(chan struct{}, 1),
		claims: make(chan claimResult, workerLimit), done: make(chan runResult, workerLimit),
	}
}

func (m *Manager) SetChangeHandlers(scheduleChanged, activityChanged func(string)) {
	m.changeMu.Lock()
	m.onChanged = scheduleChanged
	m.onActivity = activityChanged
	m.changeMu.Unlock()
}

func (m *Manager) Start(parent context.Context) {
	if m == nil || m.control == nil || m.layout == nil || m.executor == nil || !m.started.CompareAndSwap(false, true) {
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	m.wg.Add(1)
	go m.run()
	m.Resync()
}

func (m *Manager) Close() {
	if m == nil || !m.started.Load() {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Manager) Wake(userID string) {
	if m == nil || !control.ValidateUserID(userID) || m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	m.pending[userID] = struct{}{}
	m.mu.Unlock()
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) Resync() {
	if m == nil || m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	users, err := m.control.ListUsers(m.ctx)
	if err != nil {
		if m.ctx.Err() == nil {
			slog.Error("scheduler: resync users failed", "error", err)
		}
		return
	}
	for _, user := range users {
		if user.Status == control.UserStatusActive {
			m.Wake(user.ID)
		} else {
			m.StopUser(user.ID)
		}
	}
}

func (m *Manager) Stop(userID, scheduleID, runID string) bool {
	key := activeKey(userID, scheduleID)
	m.mu.Lock()
	run := m.active[key]
	m.mu.Unlock()
	if run == nil || run.runID != runID {
		return false
	}
	run.cancel(ErrRunStopped)
	return true
}

func (m *Manager) StopUser(userID string) {
	m.mu.Lock()
	runs := make([]*activeRun, 0)
	for _, run := range m.active {
		if run.userID == userID {
			runs = append(runs, run)
		}
	}
	m.mu.Unlock()
	for _, run := range runs {
		run.cancel(ErrUserStopped)
	}
}

func (m *Manager) notifySchedule(userID string) {
	m.changeMu.RLock()
	handler := m.onChanged
	m.changeMu.RUnlock()
	if handler != nil {
		handler(userID)
	}
}

func (m *Manager) notifyActivity(userID string) {
	m.changeMu.RLock()
	handler := m.onActivity
	m.changeMu.RUnlock()
	if handler != nil {
		handler(userID)
	}
}

func (m *Manager) run() {
	defer m.wg.Done()
	due := newWakeQueue()
	claiming := make(map[string]bool)
	claimWake := make(map[string]bool)
	running := 0
	var timer *time.Timer
	var timerChannel <-chan time.Time

	drainWakeups := func() {
		m.mu.Lock()
		for userID := range m.pending {
			if claiming[userID] {
				claimWake[userID] = true
			} else {
				due.SetEarlier(userID, m.now())
			}
			delete(m.pending, userID)
		}
		m.mu.Unlock()
	}
	resetTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		// A due item stays in the heap while every worker slot is occupied.
		// Disarm the timer until a claim or run completes; otherwise a due time
		// in the past would keep this loop spinning without available capacity.
		if running+len(claiming) >= m.workerLimit {
			timerChannel = nil
			return
		}
		next := due.Peek()
		if next == nil {
			timerChannel = nil
			return
		}
		delay := next.when.Sub(m.now())
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
	dispatch := func() {
		capacity := m.workerLimit - running - len(claiming)
		if capacity <= 0 {
			return
		}
		now := m.now()
		for capacity > 0 {
			candidate := due.Peek()
			if candidate == nil || candidate.when.After(now) {
				break
			}
			candidate = due.PopNext()
			claiming[candidate.userID] = true
			capacity--
			m.wg.Add(1)
			go func(userID string) {
				defer m.wg.Done()
				result := m.claimUser(userID)
				if m.ctx.Err() != nil {
					if result.state != nil {
						_ = result.state.Close()
					}
					return
				}
				select {
				case m.claims <- result:
				case <-m.ctx.Done():
					if result.state != nil {
						_ = result.state.Close()
					}
				}
			}(candidate.userID)
		}
	}

	for {
		drainWakeups()
		dispatch()
		resetTimer()
		select {
		case <-m.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-m.notify:
		case result := <-m.claims:
			delete(claiming, result.userID)
			wokenWhileClaiming := claimWake[result.userID]
			delete(claimWake, result.userID)
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					slog.Error("scheduler: claim failed", "user_id", result.userID, "error", result.err)
				}
				if wokenWhileClaiming {
					due.SetEarlier(result.userID, m.now())
				} else {
					due.SetEarlier(result.userID, m.now().Add(stateOpenRetry))
				}
				if result.state != nil {
					_ = result.state.Close()
				}
				continue
			}
			if result.claim == nil {
				if result.next != nil {
					due.SetEarlier(result.userID, *result.next)
				}
				if wokenWhileClaiming {
					due.SetEarlier(result.userID, m.now())
				}
				if result.state != nil {
					_ = result.state.Close()
				}
				continue
			}
			running++
			if result.next != nil {
				due.SetEarlier(result.userID, *result.next)
			} else {
				due.SetEarlier(result.userID, m.now())
			}
			if wokenWhileClaiming {
				due.SetEarlier(result.userID, m.now())
			}
			m.startWorker(result)
		case result := <-m.done:
			running--
			if result.err != nil && !errors.Is(result.err, context.Canceled) {
				slog.Error("scheduler: run finalization failed", "user_id", result.userID, "error", result.err)
			}
			if result.next != nil {
				due.SetEarlier(result.userID, *result.next)
			} else {
				due.SetEarlier(result.userID, m.now())
			}
		case <-timerChannel:
		}
	}
}

func (m *Manager) claimUser(userID string) claimResult {
	result := claimResult{userID: userID}
	user, err := m.control.GetUser(m.ctx, userID)
	if err != nil || user.Status != control.UserStatusActive {
		if err != nil && !errors.Is(err, control.ErrUserNotFound) {
			result.err = err
		}
		return result
	}
	state, scheduleStore, err := m.openStore(userID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			result.err = err
		}
		return result
	}
	result.state = state
	result.store = scheduleStore
	result.claim, result.err = scheduleStore.ClaimDue(m.ctx, m.now(), m.lease)
	if result.err == nil {
		result.next, result.err = scheduleStore.NextWake(m.ctx)
	}
	return result
}

func (m *Manager) startWorker(claimed claimResult) {
	claim := *claimed.claim
	timeoutContext, timeoutCancel := context.WithTimeoutCause(m.ctx, m.runTimeout, ErrRunTimeout)
	runContext, cancel := context.WithCancelCause(timeoutContext)
	key := activeKey(claimed.userID, claim.Schedule.ID)
	active := &activeRun{
		userID: claimed.userID, scheduleID: claim.Schedule.ID, runID: claim.Run.ID, cancel: cancel,
	}
	m.mu.Lock()
	m.active[key] = active
	m.mu.Unlock()
	m.notifySchedule(claimed.userID)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer timeoutCancel()
		defer cancel(nil)
		defer claimed.state.Close()
		defer func() {
			m.mu.Lock()
			if m.active[key] == active {
				delete(m.active, key)
			}
			m.mu.Unlock()
		}()

		recorder := &runRecorder{
			store: claimed.store, claim: claim, now: m.now,
			changed: func() { m.notifySchedule(claimed.userID) },
		}
		renewDone := make(chan struct{})
		renewStopped := make(chan error, 1)
		var executionReturned atomic.Bool
		go func() {
			err := m.renewLease(runContext, claimed.store, claim.Run.ID, claim.Run.Attempt, renewDone)
			if err != nil && !executionReturned.Load() && runContext.Err() == nil {
				cancel(fmt.Errorf("%w: %v", ErrRunLeaseLost, err))
			}
			renewStopped <- err
		}()
		execution := m.executor.Execute(runContext, claimed.userID, claim, recorder)
		executionReturned.Store(true)
		close(renewDone)
		leaseErr := <-renewStopped

		cause := context.Cause(runContext)
		var finalizeErr error
		switch {
		case m.ctx.Err() != nil:
			// Leave the lease durable. The next Marvo process will recover the
			// interrupted occurrence without creating a duplicate run row.
		case errors.Is(cause, ErrUserStopped):
			finalizeErr = claimed.store.InterruptRun(context.Background(), claim.Run.ID, claim.Run.Attempt, m.now(), "用户空间已停用")
		case errors.Is(execution.Error, ErrUserStopped):
			finalizeErr = claimed.store.InterruptRun(context.Background(), claim.Run.ID, claim.Run.Attempt, m.now(), "用户空间已停用")
		case errors.Is(cause, ErrRunStopped):
			_, _, finalizeErr = claimed.store.FinishCancelled(context.Background(), claim, m.now())
		case errors.Is(cause, ErrRunTimeout):
			finalizeErr = m.finishFailure(claimed, execution, true, errors.New("自动任务单次执行超时"))
		case errors.Is(cause, ErrRunLeaseLost):
			execution.Retryable = true
			if leaseErr != nil {
				slog.Warn("scheduler: run lease renewal failed", "user_id", claimed.userID, "schedule_id", claim.Schedule.ID, "error", leaseErr)
			}
			finalizeErr = m.finishFailure(claimed, execution, false, errors.New("自动任务执行状态暂时不可用"))
		case execution.Error != nil:
			finalizeErr = m.finishFailure(claimed, execution, false, execution.Error)
		default:
			_, _, finalizeErr = claimed.store.FinishSuccess(context.Background(), claim, m.now())
			if finalizeErr == nil && execution.SessionID != "" && execution.MessageID != "" && strings.TrimSpace(execution.FinalText) != "" {
				if err := m.publishFallbackActivity(claimed.userID, claimed.state, claim.Schedule, execution); err != nil {
					slog.Error("scheduler: fallback Activity failed", "user_id", claimed.userID, "schedule_id", claim.Schedule.ID, "error", err)
				}
			}
		}
		if errors.Is(finalizeErr, store.ErrScheduleConflict) || errors.Is(finalizeErr, store.ErrScheduleNotFound) {
			// Another owner reclaimed or removed this durable run. The attempt
			// fence already prevented stale state from being written.
			finalizeErr = nil
		}
		m.notifySchedule(claimed.userID)
		next, nextErr := claimed.store.NextWake(context.Background())
		if finalizeErr == nil {
			finalizeErr = nextErr
		}
		select {
		case m.done <- runResult{userID: claimed.userID, next: next, err: finalizeErr}:
		case <-m.ctx.Done():
		}
	}()
}

func (m *Manager) finishFailure(claimed claimResult, execution ExecutionResult, timedOut bool, runErr error) error {
	attempt := claimed.claim.Run.Attempt
	delayIndex := attempt - 1
	if delayIndex < 0 {
		delayIndex = 0
	}
	if delayIndex >= len(retryDelays) {
		delayIndex = len(retryDelays) - 1
	}
	updated, finishedRun, retrying, err := claimed.store.FinishFailure(context.Background(), *claimed.claim, store.ScheduleFailure{
		Error: runErr.Error(), Retryable: execution.Retryable || timedOut, TimedOut: timedOut,
		RetryDelay: retryDelays[delayIndex], MaxAttempts: len(retryDelays) + 1,
	}, m.now())
	if err != nil {
		if errors.Is(err, store.ErrScheduleNotFound) {
			return nil
		}
		return err
	}
	if !retrying && updated.SessionID != "" {
		activities, activityErr := store.NewActivityStore(claimed.state)
		if activityErr == nil {
			sourceMessageID := execution.MessageID
			if sourceMessageID == "" {
				sourceMessageID = claimed.claim.Run.RequestMessageID
			}
			if sourceMessageID == "" {
				sourceMessageID = "schedule-failure-" + claimed.claim.Run.ID
			}
			title := "“" + updated.Name + "”本轮执行失败"
			content := "自动任务本轮执行未完成。\n\n失败原因：" + finishedRun.Error
			if updated.Status == store.ScheduleStatusPaused && updated.PausedReason == "failure" &&
				finishedRun.Trigger == store.ScheduleTriggerScheduled && updated.Revision == finishedRun.ScheduleRevision {
				title = "“" + updated.Name + "”已暂停"
				content = "自动任务执行失败，已暂停后续执行。\n\n失败原因：" + updated.LastError
			}
			_, created, publishErr := activities.Publish(store.ActivityPublish{
				Kind: store.ActivityKindNotice, Title: title, Content: content,
				SourceSessionID: updated.SessionID, SourceMessageID: sourceMessageID,
			})
			if publishErr == nil && created {
				m.notifyActivity(claimed.userID)
			}
		}
	}
	return nil
}

func (m *Manager) publishFallbackActivity(userID string, state *store.StateDB, schedule store.Schedule, execution ExecutionResult) error {
	if execution.ActivityPublished {
		return nil
	}
	activities, err := store.NewActivityStore(state)
	if err != nil {
		return err
	}
	exists, err := activities.HasSourceMessage(execution.SessionID, execution.MessageID)
	if err != nil || exists {
		return err
	}
	_, created, err := activities.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: schedule.Name, Content: fallbackActivityContent(execution.FinalText),
		SourceSessionID: execution.SessionID, SourceMessageID: execution.MessageID,
	})
	if err == nil && created {
		m.notifyActivity(userID)
	}
	return err
}

func fallbackActivityContent(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= store.MaxActivityContentBytes {
		return value
	}
	suffix := "\n\n（内容较长，请打开对应的智能体对话查看完整结果。）"
	value = value[:store.MaxActivityContentBytes-len(suffix)]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value) + suffix
}

func (m *Manager) renewLease(
	ctx context.Context,
	scheduleStore *store.ScheduleStore,
	runID string,
	expectedAttempt int,
	done <-chan struct{},
) error {
	interval := m.lease / 3
	if interval < 20*time.Second {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		case <-ticker.C:
			now := m.now()
			ok, err := scheduleStore.RenewLease(ctx, runID, expectedAttempt, now.Add(m.lease), now)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("execution ownership changed")
			}
		}
	}
}

func (m *Manager) openStore(userID string) (*store.StateDB, *store.ScheduleStore, error) {
	paths, err := m.layout.UserPaths(userID)
	if err != nil {
		return nil, nil, err
	}
	databasePath := filepath.Join(paths.Workspace, ".marvo", "state.sqlite")
	info, err := os.Lstat(databasePath)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("user state database is not a regular file")
	}
	state, err := store.OpenStateDB(paths.Workspace)
	if err != nil {
		return nil, nil, err
	}
	scheduleStore, err := store.NewScheduleStore(state)
	if err != nil {
		_ = state.Close()
		return nil, nil, err
	}
	return state, scheduleStore, nil
}

type runRecorder struct {
	store   *store.ScheduleStore
	claim   store.ClaimedScheduleRun
	now     func() time.Time
	changed func()
}

func (r *runRecorder) SetSession(ctx context.Context, sessionID string) error {
	if err := r.store.SetRunSession(
		ctx, r.claim.Schedule.ID, r.claim.Run.ID, r.claim.Run.Attempt, sessionID, r.now(),
	); err != nil {
		return err
	}
	if r.changed != nil {
		r.changed()
	}
	return nil
}

func (r *runRecorder) SetRequestMessage(ctx context.Context, messageID string) error {
	return r.store.SetRunRequestMessage(ctx, r.claim.Run.ID, r.claim.Run.Attempt, messageID, r.now())
}

func (r *runRecorder) SetResponseMessage(ctx context.Context, messageID string) error {
	return r.store.SetRunResponseMessage(ctx, r.claim.Run.ID, r.claim.Run.Attempt, messageID, r.now())
}

func activeKey(userID, scheduleID string) string {
	return userID + "\x00" + scheduleID
}
