package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"marvo/config"
	"marvo/internal/agentcredentials"
	"marvo/internal/collab"
	"marvo/internal/connectors"
	"marvo/internal/control"
	"marvo/internal/delivery"
	"marvo/internal/media"
	"marvo/internal/runtimeevents"
	"marvo/internal/scheduler"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

var (
	ErrUserSpaceUnavailable = errors.New("user space unavailable")
	ErrUserSpaceDisabled    = errors.New("user space disabled")
	ErrUserSpaceMigrating   = errors.New("user space migration in progress")
)

const (
	userSpaceIdleTTL      = 5 * time.Minute
	maxIdleUserSpaces     = 32
	userSpaceReapInterval = time.Minute
)

type UserSpace struct {
	UserID      string
	Paths       userspace.Paths
	State       *store.StateDB
	NoteStore   *store.NoteStore
	Hub         *collab.Hub
	Media       *media.Manager
	Activity    *store.ActivityStore
	Connectors  *store.ConnectorStore
	Schedules   *store.ScheduleStore
	AgentDeps   *AgentDeps
	DeviceStore *store.DeviceStore
	BrandStore  *store.BrandStore
	watcher     *store.NoteWatcher
	closeOnce   sync.Once
}

type cachedUserSpace struct {
	space    *UserSpace
	leases   int
	lastUsed time.Time
	closing  bool
	closed   chan struct{}
}

type spaceInitialization struct {
	done  chan struct{}
	space *UserSpace
	err   error
}

type SpaceRegistry struct {
	config       *config.Config
	control      *control.DB
	layout       *userspace.Layout
	shuttingDown <-chan struct{}
	mu           sync.Mutex
	spaces       map[string]*cachedUserSpace
	initializing map[string]*spaceInitialization
	migrating    map[string]bool
	closed       bool
	now          func() time.Time
	idleTTL      time.Duration
	maxIdle      int
	background   context.Context
	cancel       context.CancelFunc
	backgroundWG sync.WaitGroup
	eventsOnce   sync.Once
	deliveryOnce sync.Once
	deliveries   *delivery.Dispatcher
	scheduler    *scheduler.Manager
	providers    *connectors.Registry
}

func NewSpaceRegistry(cfg *config.Config, controlDB *control.DB, layout *userspace.Layout, shuttingDown <-chan struct{}) *SpaceRegistry {
	background, cancel := context.WithCancel(context.Background())
	registry := &SpaceRegistry{
		config: cfg, control: controlDB, layout: layout, shuttingDown: shuttingDown,
		spaces: make(map[string]*cachedUserSpace), initializing: make(map[string]*spaceInitialization),
		migrating: make(map[string]bool), now: time.Now, idleTTL: userSpaceIdleTTL,
		maxIdle: maxIdleUserSpaces, background: background, cancel: cancel,
	}
	registry.providers = connectors.NewRegistry(nil)
	registry.deliveries = delivery.NewDispatcher(
		controlDB, layout, cfg.Server.SessionSecret, cfg.Server.PublicURL, registry.providers,
	)
	registry.scheduler = scheduler.NewManager(controlDB, layout, registry)
	registry.scheduler.SetChangeHandlers(registry.notifyScheduleChanged, registry.notifyScheduleActivity)
	registry.scheduler.Start(background)
	registry.backgroundWG.Add(1)
	go registry.runIdleReaper()
	return registry
}

func (r *SpaceRegistry) Acquire(ctx context.Context, userID string) (*UserSpace, func(), error) {
	space, err := r.resolve(ctx, userID)
	if err != nil {
		return nil, func() {}, err
	}
	var once sync.Once
	return space, func() {
		once.Do(func() { r.release(userID, space) })
	}, nil
}

func (r *SpaceRegistry) resolve(ctx context.Context, userID string) (*UserSpace, error) {
	user, err := r.control.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, control.ErrUserNotFound) {
			return nil, ErrUserSpaceUnavailable
		}
		return nil, err
	}
	if user.Status == control.UserStatusDisabled {
		r.CloseUser(userID)
		return nil, ErrUserSpaceDisabled
	}

	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, ErrUserSpaceUnavailable
		}
		if r.migrating[userID] {
			r.mu.Unlock()
			return nil, ErrUserSpaceMigrating
		}
		if existing := r.spaces[userID]; existing != nil {
			if existing.closing {
				done := existing.closed
				r.mu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-done:
					continue
				}
			}
			existing.lastUsed = r.now()
			existing.leases++
			space := existing.space
			r.mu.Unlock()
			return space, nil
		}
		if pending := r.initializing[userID]; pending != nil {
			done := pending.done
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				if pending.err != nil {
					return nil, pending.err
				}
				continue
			}
		}
		break
	}
	pending := &spaceInitialization{done: make(chan struct{})}
	r.initializing[userID] = pending
	r.mu.Unlock()

	space, initErr := r.initialize(userID)
	r.mu.Lock()
	if r.closed && space != nil {
		space.Close()
		space = nil
		initErr = ErrUserSpaceUnavailable
	} else if initErr == nil {
		entry := &cachedUserSpace{space: space, leases: 1, lastUsed: r.now(), closed: make(chan struct{})}
		r.spaces[userID] = entry
	}
	pending.space = space
	pending.err = initErr
	delete(r.initializing, userID)
	close(pending.done)
	r.mu.Unlock()
	return space, initErr
}

func (r *SpaceRegistry) release(userID string, space *UserSpace) {
	r.mu.Lock()
	entry := r.spaces[userID]
	if entry == nil || entry.space != space || entry.leases == 0 {
		r.mu.Unlock()
		return
	}
	entry.leases--
	entry.lastUsed = r.now()
	if entry.leases == 0 && entry.closing {
		delete(r.spaces, userID)
		close(entry.closed)
		r.mu.Unlock()
		space.Close()
		return
	}
	closeSpaces := r.pruneIdleLocked(entry.lastUsed, false)
	r.mu.Unlock()
	closeUserSpaces(closeSpaces)
}

func (r *SpaceRegistry) initialize(userID string) (*UserSpace, error) {
	paths, err := r.layout.EnsureUser(userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUserSpaceUnavailable, err)
	}
	if err := EnsureThemeFile(paths.Workspace); err != nil {
		return nil, fmt.Errorf("%w: initialize theme: %v", ErrUserSpaceUnavailable, err)
	}
	noteStore := store.NewNoteStore(paths.Workspace)
	mediaManager := media.NewManager(noteStore)
	hub := collab.NewHub()
	stateDB, err := store.OpenStateDB(paths.Workspace)
	if err != nil {
		mediaManager.Close()
		hub.Close()
		return nil, fmt.Errorf("%w: initialize user state: %v", ErrUserSpaceUnavailable, err)
	}
	activityStore, err := store.NewActivityStore(stateDB)
	if err != nil {
		_ = stateDB.Close()
		mediaManager.Close()
		hub.Close()
		return nil, fmt.Errorf("%w: initialize Activity: %v", ErrUserSpaceUnavailable, err)
	}
	connectorStore, err := store.NewConnectorStore(stateDB, userID, r.config.Server.SessionSecret)
	if err != nil {
		_ = stateDB.Close()
		mediaManager.Close()
		hub.Close()
		return nil, fmt.Errorf("%w: initialize Activity connectors: %v", ErrUserSpaceUnavailable, err)
	}
	scheduleStore, err := store.NewScheduleStore(stateDB)
	if err != nil {
		_ = stateDB.Close()
		mediaManager.Close()
		hub.Close()
		return nil, fmt.Errorf("%w: initialize automatic tasks: %v", ErrUserSpaceUnavailable, err)
	}
	space := &UserSpace{
		UserID: userID, Paths: paths, State: stateDB, NoteStore: noteStore, Hub: hub, Media: mediaManager,
		Activity:    activityStore,
		Connectors:  connectorStore,
		Schedules:   scheduleStore,
		DeviceStore: store.NewDeviceStore(stateDB, derivedUserSecret(r.config.Server.SessionSecret, userID, "device")),
	}
	cleanup := func() {
		space.Close()
	}
	brandStore, err := store.NewBrandStore(stateDB)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load brand settings: %v", ErrUserSpaceUnavailable, err)
	}
	space.BrandStore = brandStore

	settings, err := store.NewAgentSettingsStore(stateDB)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load Agent settings: %v", ErrUserSpaceUnavailable, err)
	}
	credentials, err := agentcredentials.NewStore(paths.OpenCodeData, userID, r.config.Runtime.Token)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: initialize Agent credentials: %v", ErrUserSpaceUnavailable, err)
	}
	memories, err := store.NewMemoryStore(stateDB)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load Agent memories: %v", ErrUserSpaceUnavailable, err)
	}
	globalPrompt, err := store.NewAgentGlobalPromptFile(filepath.Join(paths.AgentHome, ".config", "opencode", "AGENTS.md"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: initialize Agent prompt: %v", ErrUserSpaceUnavailable, err)
	}
	runtimeURL := r.config.Runtime.URL + "/user/" + userID
	space.AgentDeps = NewAgentDeps(runtimeURL, r.shuttingDown, settings, memories, globalPrompt, activityStore)
	space.AgentDeps.SetUpstreamBearer(r.config.Runtime.Token)
	space.AgentDeps.SetCredentialStore(credentials)
	space.AgentDeps.SetActivityChangeHandler(func() {
		unread, pending, countErr := activityStore.Counts()
		if countErr == nil {
			hub.BroadcastAll(store.MustJSON(map[string]any{
				"action": "activity_changed", "unread": unread, "pending": pending,
			}))
		}
	})
	mediaManager.SetChangeHandler(func(title string, asset media.Asset) {
		hub.BroadcastToNote(title, "", store.MustJSON(map[string]any{
			"action": "asset_changed", "title": title, "asset": asset,
		}))
	})
	watcher, err := store.WatchNotes(paths.Workspace, func(title string) {
		snapshot, snapshotErr := noteStore.Snapshot(title)
		if snapshotErr != nil {
			return
		}
		mediaManager.ReconcileNote(title, snapshot.InstanceToken)
		hub.BroadcastToNote(title, "", store.MustJSON(map[string]any{
			"action": "note_changed", "title": title, "note": snapshot.Note,
			"content": snapshot.Content, "content_revision": snapshot.ContentRevision,
			"meta_revision": snapshot.MetaRevision, "instance_token": snapshot.InstanceToken,
		}))
	}, func() {
		hub.BroadcastAll(store.MustJSON(map[string]any{"action": "note_list_changed"}))
	}, func() {
		hub.BroadcastAll(store.MustJSON(map[string]any{"action": "theme_changed"}))
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: start note watcher: %v", ErrUserSpaceUnavailable, err)
	}
	space.watcher = watcher
	return space, nil
}

func (r *SpaceRegistry) CloseUser(userID string) {
	r.mu.Lock()
	entry := r.spaces[userID]
	if entry != nil && entry.leases > 0 {
		entry.closing = true
		r.mu.Unlock()
		// End SSE/poll requests immediately so they cannot keep their own lease
		// alive forever. State and media remain valid until short-lived users and
		// the cancelling scheduler execution release their references.
		if entry.space.Hub != nil {
			entry.space.Hub.Close()
		}
		return
	}
	delete(r.spaces, userID)
	if entry != nil {
		close(entry.closed)
	}
	r.mu.Unlock()
	if entry != nil {
		entry.space.Close()
	}
}

func (r *SpaceRegistry) BeginMigration(ctx context.Context, userID string) (func(), error) {
	for {
		r.mu.Lock()
		if r.closed || r.migrating[userID] {
			r.mu.Unlock()
			return nil, ErrUserSpaceUnavailable
		}
		if pending := r.initializing[userID]; pending != nil {
			done := pending.done
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		r.migrating[userID] = true
		entry := r.spaces[userID]
		delete(r.spaces, userID)
		if entry != nil {
			close(entry.closed)
		}
		r.mu.Unlock()
		if entry != nil {
			entry.space.Close()
		}
		return func() {
			r.mu.Lock()
			delete(r.migrating, userID)
			r.mu.Unlock()
		}, nil
	}
}

func (r *SpaceRegistry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.cancel()
	spaces := make([]*UserSpace, 0, len(r.spaces))
	for _, entry := range r.spaces {
		spaces = append(spaces, entry.space)
		close(entry.closed)
	}
	r.spaces = make(map[string]*cachedUserSpace)
	r.mu.Unlock()
	if r.scheduler != nil {
		r.scheduler.Close()
	}
	r.backgroundWG.Wait()
	if r.deliveries != nil {
		r.deliveries.Wait()
	}
	for _, space := range spaces {
		space.Close()
	}
}

func (s *UserSpace) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.watcher != nil {
			_ = s.watcher.Close()
		}
		if s.Media != nil {
			s.Media.Close()
		}
		if s.Hub != nil {
			s.Hub.Close()
		}
		if s.State != nil {
			_ = s.State.Close()
		}
	})
}

func (r *SpaceRegistry) runIdleReaper() {
	defer r.backgroundWG.Done()
	ticker := time.NewTicker(userSpaceReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.pruneIdle(true)
		case <-r.background.Done():
			return
		case <-r.shuttingDown:
			return
		}
	}
}

func (r *SpaceRegistry) pruneIdle(expire bool) {
	r.mu.Lock()
	spaces := r.pruneIdleLocked(r.now(), expire)
	r.mu.Unlock()
	closeUserSpaces(spaces)
}

func (r *SpaceRegistry) pruneIdleLocked(now time.Time, expire bool) []*UserSpace {
	type candidate struct {
		userID   string
		lastUsed time.Time
	}
	candidates := make([]candidate, 0, len(r.spaces))
	idleCount := 0
	for userID, entry := range r.spaces {
		if entry.leases != 0 {
			continue
		}
		idleCount++
		if entry.space.Media != nil && !entry.space.Media.Idle() {
			continue
		}
		candidates = append(candidates, candidate{userID: userID, lastUsed: entry.lastUsed})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastUsed.Before(candidates[j].lastUsed) })
	removed := make(map[string]bool)
	spaces := make([]*UserSpace, 0)
	remove := func(userID string) {
		entry := r.spaces[userID]
		if entry == nil || entry.leases != 0 || removed[userID] {
			return
		}
		delete(r.spaces, userID)
		close(entry.closed)
		removed[userID] = true
		idleCount--
		spaces = append(spaces, entry.space)
	}
	if expire && r.idleTTL > 0 {
		for _, candidate := range candidates {
			if now.Sub(candidate.lastUsed) >= r.idleTTL {
				remove(candidate.userID)
			}
		}
	}
	if r.maxIdle >= 0 {
		for _, candidate := range candidates {
			if idleCount <= r.maxIdle {
				break
			}
			remove(candidate.userID)
		}
	}
	return spaces
}

func closeUserSpaces(spaces []*UserSpace) {
	for _, space := range spaces {
		space.Close()
	}
}

func (r *SpaceRegistry) withLoadedSpace(userID string, visit func(*UserSpace)) bool {
	r.mu.Lock()
	entry := r.spaces[userID]
	if entry == nil || entry.closing || r.closed {
		r.mu.Unlock()
		return false
	}
	entry.leases++
	entry.lastUsed = r.now()
	space := entry.space
	r.mu.Unlock()
	defer r.release(userID, space)
	visit(space)
	return true
}

func (r *SpaceRegistry) notifyRuntimeEvent(event runtimeevents.Event) {
	if !event.Valid() {
		return
	}
	if event.Kind == runtimeevents.KindActivity && r.deliveries != nil {
		r.deliveries.Wake(event.UserID)
	}
	if event.Kind == runtimeevents.KindSchedules && r.scheduler != nil {
		r.scheduler.Wake(event.UserID)
	}
	r.withLoadedSpace(event.UserID, func(space *UserSpace) {
		space.broadcastRuntimeEvent(event.Kind)
	})
}

func (r *SpaceRegistry) StartDeliveries() {
	r.deliveryOnce.Do(func() {
		if r.deliveries != nil {
			r.deliveries.Start(r.background)
		}
	})
}

func (r *SpaceRegistry) ResyncDeliveries() {
	if r.deliveries != nil {
		r.deliveries.Resync(r.background)
	}
}

func (r *SpaceRegistry) WakeDeliveries(userID string) {
	if r.deliveries != nil {
		r.deliveries.Wake(userID)
	}
}

func (s *UserSpace) broadcastRuntimeEvent(kind runtimeevents.Kind) {
	if s == nil || s.Hub == nil {
		return
	}
	switch kind {
	case runtimeevents.KindActivity:
		unread, pending, err := s.Activity.Counts()
		if err == nil {
			s.Hub.BroadcastAll(store.MustJSON(map[string]any{
				"action": "activity_changed", "unread": unread, "pending": pending,
			}))
		}
	case runtimeevents.KindSpace:
		s.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "brand_changed", "brand": s.BrandStore.Get()}))
	case runtimeevents.KindMemories:
		s.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "agent_memories_changed"}))
	case runtimeevents.KindAgentSettings:
		s.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "agent_settings_changed"}))
	case runtimeevents.KindDevices:
		s.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "devices_changed"}))
	case runtimeevents.KindSchedules:
		s.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "schedules_changed"}))
	}
}

func (r *SpaceRegistry) notifyScheduleChanged(userID string) {
	r.withLoadedSpace(userID, func(space *UserSpace) {
		space.broadcastRuntimeEvent(runtimeevents.KindSchedules)
	})
}

func (r *SpaceRegistry) notifyScheduleActivity(userID string) {
	if r.deliveries != nil {
		r.deliveries.Wake(userID)
	}
	r.withLoadedSpace(userID, func(space *UserSpace) {
		space.broadcastRuntimeEvent(runtimeevents.KindActivity)
	})
}

func (r *SpaceRegistry) WakeSchedules(userID string) {
	if r != nil && r.scheduler != nil {
		r.scheduler.Wake(userID)
	}
}

func (r *SpaceRegistry) StopSchedule(userID, scheduleID, runID string) bool {
	return r != nil && r.scheduler != nil && r.scheduler.Stop(userID, scheduleID, runID)
}

func (r *SpaceRegistry) StopUserSchedules(userID string) {
	if r != nil && r.scheduler != nil {
		r.scheduler.StopUser(userID)
	}
}

func derivedUserSecret(secret, userID, purpose string) string {
	return signPayload("marvo-user-secret:"+purpose+":"+userID, secret)
}

type userSpaceContextKey struct{}

func (d *Dependencies) UserSpaceMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.PathValue("userID")
			space, release, err := d.Spaces.Acquire(r.Context(), userID)
			if errors.Is(err, ErrUserSpaceDisabled) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
				return
			}
			if errors.Is(err, ErrUserSpaceMigrating) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "user space migration in progress"})
				return
			}
			if errors.Is(err, ErrUserSpaceUnavailable) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
				return
			}
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "user space unavailable"})
				return
			}
			defer release()
			ctx := context.WithValue(r.Context(), userSpaceContextKey{}, space)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func userSpaceFromRequest(r *http.Request) *UserSpace {
	space, _ := r.Context().Value(userSpaceContextKey{}).(*UserSpace)
	return space
}

func (d *Dependencies) Scoped(handler scopedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		space := userSpaceFromRequest(r)
		if space == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "missing user context"})
			return
		}
		configCopy := *d.Config
		configCopy.Server.DataDir = space.Paths.Workspace
		scoped := &Dependencies{
			Config:       &configCopy,
			Control:      d.Control,
			Layout:       d.Layout,
			Spaces:       d.Spaces,
			UserID:       space.UserID,
			NoteStore:    space.NoteStore,
			Hub:          space.Hub,
			Media:        space.Media,
			State:        space.State,
			Activity:     space.Activity,
			Connectors:   space.Connectors,
			Schedules:    space.Schedules,
			Providers:    d.Spaces.providers,
			AgentDeps:    space.AgentDeps,
			DeviceStore:  space.DeviceStore,
			BrandStore:   space.BrandStore,
			securityRoot: d,
		}
		handler(scoped, w, r)
	})
}

func (d *Dependencies) UserDeviceMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			space := userSpaceFromRequest(r)
			if space != nil && validateUserDeviceCookie(r, space) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		})
	}
}

func validateUserDeviceCookie(r *http.Request, space *UserSpace) bool {
	cookie, err := r.Cookie(userDeviceCookieName(space.UserID))
	if err != nil || cookie.Value == "" {
		return false
	}
	token, signature, found := cutLast(cookie.Value, ":")
	return found && space.DeviceStore.VerifyToken(token, signature)
}

func userDeviceCookieName(userID string) string {
	return "marvo_user_device_" + userID
}
