package handler

import (
	"marvo/internal/collab"
	"marvo/internal/control"
	"marvo/internal/media"
	"marvo/internal/store"
	"marvo/internal/userspace"
	"net/http"
	"sync"
	"time"

	"marvo/config"
)

type Dependencies struct {
	Config       *config.Config
	Control      *control.DB
	Layout       *userspace.Layout
	Spaces       *SpaceRegistry
	UserID       string
	NoteStore    *store.NoteStore
	Hub          *collab.Hub
	Media        *media.Manager
	AgentDeps    *AgentDeps
	DeviceStore  *store.DeviceStore
	Legacy       userspace.LegacySources
	migrationMu  sync.Mutex
	securityMu   sync.Mutex
	rateLimits   map[string]rateWindow
	challenges   map[string]int64
	securityRoot *Dependencies
}

type rateWindow struct {
	Count int
	Reset time.Time
}

func RegisterRoutes(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Platform administrator authentication remains separate from every user's
	// own management session and never grants access to user content.
	mux.HandleFunc("POST /api/platform/auth/verify", deps.Verify)
	mux.HandleFunc("POST /api/platform/auth", deps.Login)
	mux.HandleFunc("POST /api/platform/auth/logout", deps.Logout)
	if deps.Control != nil {
		mux.HandleFunc("POST /api/user/{userID}/auth/verify", deps.VerifyUser)
		mux.HandleFunc("POST /api/user/{userID}/auth", deps.LoginUser)
		mux.HandleFunc("POST /api/user/{userID}/auth/logout", deps.LogoutUser)
	}
	if deps.Spaces != nil {
		registerUserRoutes(mux, deps)
	}

	admin := deps.AdminMiddleware()
	if deps.Control != nil && deps.Layout != nil {
		mux.Handle("GET /api/admin/users", admin(http.HandlerFunc(deps.ListUsers)))
		mux.Handle("POST /api/admin/users", admin(http.HandlerFunc(deps.CreateUser)))
		mux.Handle("PUT /api/admin/users/{userID}/status", admin(http.HandlerFunc(deps.UpdateUserStatus)))
		mux.Handle("POST /api/admin/users/{userID}/credentials", admin(http.HandlerFunc(deps.ResetUserCredentials)))
		mux.Handle("GET /api/admin/legacy-migration", admin(http.HandlerFunc(deps.LegacyMigrationStatus)))
		mux.Handle("POST /api/admin/users/{userID}/migrate-legacy", admin(http.HandlerFunc(deps.MigrateLegacyUser)))
	}
}

type scopedHandler func(*Dependencies, http.ResponseWriter, *http.Request)

func registerUserRoutes(mux *http.ServeMux, deps *Dependencies) {
	space := deps.UserSpaceMiddleware()
	device := deps.UserDeviceMiddleware()
	userAdmin := deps.UserAdminMiddleware()
	scoped := deps.Scoped
	content := func(handler scopedHandler) http.Handler {
		return space(device(scoped(handler)))
	}
	management := func(handler scopedHandler) http.Handler {
		return space(userAdmin(scoped(handler)))
	}
	userPublic := func(handler scopedHandler) http.Handler {
		return space(scoped(handler))
	}

	mux.Handle("POST /api/user/{userID}/auth/apply", userPublic((*Dependencies).Apply))
	mux.Handle("GET /api/user/{userID}/auth/token", userPublic((*Dependencies).Token))

	mux.Handle("GET /api/user/{userID}/notes", content((*Dependencies).ListNotes))
	mux.Handle("GET /api/user/{userID}/notes/{title}", content((*Dependencies).GetNote))
	mux.Handle("GET /api/user/{userID}/notes/{title}/assets/{filename}", content((*Dependencies).GetAttachment))
	mux.Handle("GET /api/user/{userID}/theme", content((*Dependencies).GetTheme))
	mux.Handle("GET /api/user/{userID}/search", content((*Dependencies).SearchNotes))
	mux.Handle("POST /api/user/{userID}/notes", content((*Dependencies).CreateNote))
	mux.Handle("PUT /api/user/{userID}/notes/{title}/content", content((*Dependencies).UpdateNoteContent))
	mux.Handle("PUT /api/user/{userID}/notes/{title}/meta", content((*Dependencies).UpdateNoteMeta))
	mux.Handle("PUT /api/user/{userID}/notes/{title}/rename", content((*Dependencies).RenameNote))
	mux.Handle("DELETE /api/user/{userID}/notes/{title}", content((*Dependencies).DeleteNote))
	mux.Handle("GET /api/user/{userID}/notes/{title}/assets", content((*Dependencies).ListMediaAssets))
	mux.Handle("POST /api/user/{userID}/notes/{title}/assets/reserve", content((*Dependencies).ReserveMediaAsset))
	mux.Handle("GET /api/user/{userID}/notes/{title}/assets/{assetID}/status", content((*Dependencies).GetMediaAsset))
	mux.Handle("PUT /api/user/{userID}/notes/{title}/assets/{assetID}/content", content((*Dependencies).UploadMediaAsset))
	mux.Handle("DELETE /api/user/{userID}/notes/{title}/assets/{assetID}", content((*Dependencies).AbandonMediaAsset))
	mux.Handle("GET /api/user/{userID}/trash", content((*Dependencies).ListTrash))
	mux.Handle("POST /api/user/{userID}/trash/{id}/restore", content((*Dependencies).RestoreTrash))
	mux.Handle("DELETE /api/user/{userID}/trash/{id}", content((*Dependencies).PermanentlyDeleteTrash))
	mux.Handle("DELETE /api/user/{userID}/trash", content((*Dependencies).EmptyTrash))
	mux.Handle("GET /api/user/{userID}/events", content((*Dependencies).HandleSSE))
	mux.Handle("POST /api/user/{userID}/send", content((*Dependencies).HandleSend))

	mux.Handle("GET /api/user/{userID}/agent/settings", content((*Dependencies).AgentGetSettings))
	mux.Handle("PUT /api/user/{userID}/agent/settings", content((*Dependencies).AgentUpdateSettings))
	mux.Handle("GET /api/user/{userID}/agent/personalization", content((*Dependencies).AgentGetPersonalization))
	mux.Handle("PUT /api/user/{userID}/agent/personalization", content((*Dependencies).AgentUpdatePersonalization))
	mux.Handle("GET /api/user/{userID}/agent/providers", content((*Dependencies).AgentListProviders))
	mux.Handle("POST /api/user/{userID}/agent/providers/{providerID}/connect/key", content((*Dependencies).AgentConnectProviderKey))
	mux.Handle("POST /api/user/{userID}/agent/providers/{providerID}/connect/oauth", content((*Dependencies).AgentStartProviderOAuth))
	mux.Handle("DELETE /api/user/{userID}/agent/providers/{providerID}", content((*Dependencies).AgentDisconnectProvider))
	mux.Handle("GET /api/user/{userID}/agent/provider-attempts/{attemptID}", content((*Dependencies).AgentGetProviderOAuthAttempt))
	mux.Handle("POST /api/user/{userID}/agent/provider-attempts/{attemptID}/complete", content((*Dependencies).AgentCompleteProviderOAuth))
	mux.Handle("DELETE /api/user/{userID}/agent/provider-attempts/{attemptID}", content((*Dependencies).AgentCancelProviderOAuth))
	mux.Handle("GET /api/user/{userID}/agent/global/event", content((*Dependencies).AgentProxyGlobalSSE))
	mux.Handle("GET /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("POST /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("PATCH /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("PUT /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("DELETE /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))

	mux.Handle("GET /api/user/{userID}/admin/requests", management((*Dependencies).ListRequests))
	mux.Handle("POST /api/user/{userID}/admin/requests/{id}/approve", management((*Dependencies).ApproveRequest))
	mux.Handle("POST /api/user/{userID}/admin/requests/{id}/reject", management((*Dependencies).RejectRequest))
	mux.Handle("GET /api/user/{userID}/admin/devices", management((*Dependencies).ListDevices))
	mux.Handle("DELETE /api/user/{userID}/admin/devices/{id}", management((*Dependencies).RevokeDevice))
}
