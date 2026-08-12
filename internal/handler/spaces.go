package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"marvo/config"
	"marvo/internal/agentcredentials"
	"marvo/internal/collab"
	"marvo/internal/control"
	"marvo/internal/media"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

var (
	ErrUserSpaceUnavailable = errors.New("user space unavailable")
	ErrUserSpaceDisabled    = errors.New("user space disabled")
	ErrUserSpaceMigrating   = errors.New("user space migration in progress")
)

type UserSpace struct {
	UserID      string
	Paths       userspace.Paths
	NoteStore   *store.NoteStore
	Hub         *collab.Hub
	Media       *media.Manager
	AgentDeps   *AgentDeps
	DeviceStore *store.DeviceStore
	BrandStore  *store.BrandStore
	watcher     *store.NoteWatcher
	closeOnce   sync.Once
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
	spaces       map[string]*UserSpace
	initializing map[string]*spaceInitialization
	migrating    map[string]bool
	closed       bool
}

func NewSpaceRegistry(cfg *config.Config, controlDB *control.DB, layout *userspace.Layout, shuttingDown <-chan struct{}) *SpaceRegistry {
	return &SpaceRegistry{
		config: cfg, control: controlDB, layout: layout, shuttingDown: shuttingDown,
		spaces: make(map[string]*UserSpace), initializing: make(map[string]*spaceInitialization),
		migrating: make(map[string]bool),
	}
}

func (r *SpaceRegistry) Resolve(ctx context.Context, userID string) (*UserSpace, error) {
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
		r.mu.Unlock()
		return existing, nil
	}
	if pending := r.initializing[userID]; pending != nil {
		done := pending.done
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
			return pending.space, pending.err
		}
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
		r.spaces[userID] = space
	}
	pending.space = space
	pending.err = initErr
	delete(r.initializing, userID)
	close(pending.done)
	r.mu.Unlock()
	return space, initErr
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
	space := &UserSpace{
		UserID: userID, Paths: paths, NoteStore: noteStore, Hub: hub, Media: mediaManager,
		DeviceStore: store.NewDeviceStore(paths.App, derivedUserSecret(r.config.Server.SessionSecret, userID, "device")),
	}
	cleanup := func() {
		space.Close()
	}
	brandStore, err := store.NewBrandStore(paths.App)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load brand settings: %v", ErrUserSpaceUnavailable, err)
	}
	space.BrandStore = brandStore

	settings, err := store.NewAgentSettingsStore(paths.App)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load Agent settings: %v", ErrUserSpaceUnavailable, err)
	}
	credentials, err := agentcredentials.NewStore(paths.App, userID, r.config.Runtime.Token)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: initialize Agent credentials: %v", ErrUserSpaceUnavailable, err)
	}
	personalization, err := store.NewAgentPersonalizationStore(paths.Workspace)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: load Agent personalization: %v", ErrUserSpaceUnavailable, err)
	}
	globalPrompt, err := store.NewAgentGlobalPromptFile(filepath.Join(paths.Agent, "home", ".config", "opencode", "AGENTS.md"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w: initialize Agent prompt: %v", ErrUserSpaceUnavailable, err)
	}
	runtimeURL := r.config.Runtime.URL + "/user/" + userID
	space.AgentDeps = NewAgentDeps(runtimeURL, r.shuttingDown, settings, personalization, globalPrompt)
	space.AgentDeps.SetUpstreamBearer(r.config.Runtime.Token)
	space.AgentDeps.SetCredentialStore(credentials)
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
	space := r.spaces[userID]
	delete(r.spaces, userID)
	r.mu.Unlock()
	if space != nil {
		space.Close()
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
		space := r.spaces[userID]
		delete(r.spaces, userID)
		r.mu.Unlock()
		if space != nil {
			space.Close()
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
	spaces := make([]*UserSpace, 0, len(r.spaces))
	for _, space := range r.spaces {
		spaces = append(spaces, space)
	}
	r.spaces = make(map[string]*UserSpace)
	r.mu.Unlock()
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
	})
}

func derivedUserSecret(secret, userID, purpose string) string {
	return signPayload("marvo-user-secret:"+purpose+":"+userID, secret)
}

type userSpaceContextKey struct{}

func (d *Dependencies) UserSpaceMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.PathValue("userID")
			space, err := d.Spaces.Resolve(r.Context(), userID)
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
