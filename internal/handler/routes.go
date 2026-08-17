package handler

import (
	"marvo/internal/apprelease"
	"marvo/internal/collab"
	"marvo/internal/connectors"
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
	State        *store.StateDB
	Activity     *store.ActivityStore
	Connectors   *store.ConnectorStore
	Schedules    *store.ScheduleStore
	Providers    *connectors.Registry
	AgentDeps    *AgentDeps
	DeviceStore  *store.DeviceStore
	BrandStore   *store.BrandStore
	Legacy       userspace.LegacySources
	AppReleases  *apprelease.Store
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
	mux.HandleFunc("GET /api/app/android/release", deps.GetAndroidRelease)
	mux.HandleFunc("GET /api/app/android/apk", deps.DownloadAndroidAPK)

	// Platform administrator authentication remains separate from every user's
	// own management session and never grants access to user content.
	mux.HandleFunc("POST /api/platform/auth/verify", deps.Verify)
	mux.HandleFunc("POST /api/platform/auth", deps.Login)
	mux.HandleFunc("POST /api/platform/auth/logout", deps.Logout)
	if deps.Control != nil {
		mux.HandleFunc("GET /api/user/{userID}/identity", deps.GetUserIdentity)
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
		mux.Handle("GET /api/admin/android/release", admin(http.HandlerFunc(deps.GetAndroidRelease)))
		mux.Handle("PUT /api/admin/android/release", admin(http.HandlerFunc(deps.PublishAndroidRelease)))
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
	mux.Handle("GET /api/user/{userID}/brand", content((*Dependencies).GetBrand))
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
	mux.Handle("GET /api/user/{userID}/activity", content((*Dependencies).ListActivities))
	mux.Handle("GET /api/user/{userID}/activity/counts", content((*Dependencies).ActivityCounts))
	mux.Handle("GET /api/user/{userID}/activity/{id}", content((*Dependencies).GetActivity))
	mux.Handle("POST /api/user/{userID}/activity/read", content((*Dependencies).MarkActivitiesRead))
	mux.Handle("DELETE /api/user/{userID}/activity/{id}", content((*Dependencies).DeleteActivity))
	mux.Handle("GET /api/user/{userID}/schedules", content((*Dependencies).ListSchedules))
	mux.Handle("POST /api/user/{userID}/schedules", content((*Dependencies).CreateSchedule))
	mux.Handle("GET /api/user/{userID}/schedules/{id}", content((*Dependencies).GetSchedule))
	mux.Handle("PUT /api/user/{userID}/schedules/{id}", content((*Dependencies).UpdateSchedule))
	mux.Handle("DELETE /api/user/{userID}/schedules/{id}", content((*Dependencies).DeleteSchedule))
	mux.Handle("POST /api/user/{userID}/schedules/{id}/pause", content((*Dependencies).PauseSchedule))
	mux.Handle("POST /api/user/{userID}/schedules/{id}/resume", content((*Dependencies).ResumeSchedule))
	mux.Handle("POST /api/user/{userID}/schedules/{id}/complete", content((*Dependencies).CompleteSchedule))
	mux.Handle("POST /api/user/{userID}/schedules/{id}/run", content((*Dependencies).RunScheduleNow))
	mux.Handle("POST /api/user/{userID}/schedules/{id}/stop", content((*Dependencies).StopScheduleRun))
	mux.Handle("GET /api/user/{userID}/schedules/{id}/runs", content((*Dependencies).ListScheduleRuns))

	mux.Handle("GET /api/user/{userID}/agent/settings", management((*Dependencies).AgentGetSettings))
	mux.Handle("PUT /api/user/{userID}/agent/settings", management((*Dependencies).AgentUpdateSettings))
	mux.Handle("GET /api/user/{userID}/agent/memories", management((*Dependencies).AgentGetMemories))
	mux.Handle("PUT /api/user/{userID}/agent/memories", management((*Dependencies).AgentUpdateMemories))
	mux.Handle("GET /api/user/{userID}/agent/providers", management((*Dependencies).AgentListProviders))
	mux.Handle("POST /api/user/{userID}/agent/providers/{providerID}/connect/key", management((*Dependencies).AgentConnectProviderKey))
	mux.Handle("POST /api/user/{userID}/agent/providers/{providerID}/connect/oauth", management((*Dependencies).AgentStartProviderOAuth))
	mux.Handle("DELETE /api/user/{userID}/agent/providers/{providerID}", management((*Dependencies).AgentDisconnectProvider))
	mux.Handle("GET /api/user/{userID}/agent/provider-attempts/{attemptID}", management((*Dependencies).AgentGetProviderOAuthAttempt))
	mux.Handle("POST /api/user/{userID}/agent/provider-attempts/{attemptID}/complete", management((*Dependencies).AgentCompleteProviderOAuth))
	mux.Handle("DELETE /api/user/{userID}/agent/provider-attempts/{attemptID}", management((*Dependencies).AgentCancelProviderOAuth))
	mux.Handle("GET /api/user/{userID}/agent/global/event", content((*Dependencies).AgentProxyGlobalSSE))
	mux.Handle("GET /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("POST /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("PATCH /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("PUT /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))
	mux.Handle("DELETE /api/user/{userID}/agent/{path...}", content((*Dependencies).AgentProxyJSON))

	mux.Handle("GET /api/user/{userID}/admin/requests", management((*Dependencies).ListRequests))
	mux.Handle("GET /api/user/{userID}/admin/me", management((*Dependencies).GetUserAdminIdentity))
	mux.Handle("GET /api/user/{userID}/admin/space", management((*Dependencies).GetSpaceInfo))
	mux.Handle("GET /api/user/{userID}/admin/brand", management((*Dependencies).GetBrand))
	mux.Handle("PUT /api/user/{userID}/admin/brand", management((*Dependencies).UpdateBrand))
	mux.Handle("GET /api/user/{userID}/admin/security", management((*Dependencies).GetUserSecurity))
	mux.Handle("PUT /api/user/{userID}/admin/security/password", management((*Dependencies).ChangeUserPassword))
	mux.Handle("POST /api/user/{userID}/admin/security/totp", management((*Dependencies).BeginUserTOTPEnrollment))
	mux.Handle("POST /api/user/{userID}/admin/security/totp/confirm", management((*Dependencies).ConfirmUserTOTPEnrollment))
	mux.Handle("DELETE /api/user/{userID}/admin/security/totp", management((*Dependencies).DisableUserTOTP))
	mux.Handle("POST /api/user/{userID}/admin/requests/{id}/approve", management((*Dependencies).ApproveRequest))
	mux.Handle("POST /api/user/{userID}/admin/requests/{id}/reject", management((*Dependencies).RejectRequest))
	mux.Handle("GET /api/user/{userID}/admin/devices", management((*Dependencies).ListDevices))
	mux.Handle("PATCH /api/user/{userID}/admin/devices/{id}", management((*Dependencies).RenameDevice))
	mux.Handle("DELETE /api/user/{userID}/admin/devices/{id}", management((*Dependencies).RevokeDevice))
	mux.Handle("GET /api/user/{userID}/admin/connectors/providers", management((*Dependencies).ListConnectorProviders))
	mux.Handle("GET /api/user/{userID}/admin/connectors", management((*Dependencies).ListConnectors))
	mux.Handle("POST /api/user/{userID}/admin/connectors", management((*Dependencies).CreateConnector))
	mux.Handle("PUT /api/user/{userID}/admin/connectors/{id}", management((*Dependencies).UpdateConnector))
	mux.Handle("DELETE /api/user/{userID}/admin/connectors/{id}", management((*Dependencies).DeleteConnector))
	mux.Handle("POST /api/user/{userID}/admin/connectors/test", management((*Dependencies).TestConnector))
	mux.Handle("POST /api/user/{userID}/admin/connectors/{id}/retry", management((*Dependencies).RetryConnectorDeliveries))
}
