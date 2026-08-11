package handler

import (
	"marvo/internal/collab"
	"marvo/internal/media"
	"marvo/internal/store"
	"net/http"
	"sync"
	"time"

	"marvo/config"
)

type Dependencies struct {
	Config      *config.Config
	NoteStore   *store.NoteStore
	Hub         *collab.Hub
	Media       *media.Manager
	AgentDeps   *AgentDeps
	DeviceStore *store.DeviceStore
	securityMu  sync.Mutex
	rateLimits  map[string]rateWindow
	challenges  map[string]int64
}

type rateWindow struct {
	Count int
	Reset time.Time
}

func RegisterRoutes(mux *http.ServeMux, deps *Dependencies) {
	mux.HandleFunc("POST /api/auth/verify", deps.Verify)
	mux.HandleFunc("POST /api/auth", deps.Login)
	mux.HandleFunc("POST /api/auth/logout", deps.Logout)
	mux.HandleFunc("POST /api/auth/apply", deps.Apply)
	mux.HandleFunc("GET /api/auth/token", deps.Token)

	auth := deps.AuthMiddleware()
	admin := deps.AdminMiddleware()

	// Content access is deliberately device-only. An administrator session can
	// approve or revoke devices, but cannot read notes unless this browser is an
	// independently approved device too.
	mux.Handle("GET /api/notes", auth(http.HandlerFunc(deps.ListNotes)))
	mux.Handle("GET /api/notes/{title}", auth(http.HandlerFunc(deps.GetNote)))
	mux.Handle("GET /api/notes/{title}/assets/{filename}", auth(http.HandlerFunc(deps.GetAttachment)))
	mux.Handle("GET /api/theme", auth(http.HandlerFunc(deps.GetTheme)))
	mux.Handle("GET /api/search", auth(http.HandlerFunc(deps.SearchNotes)))

	mux.Handle("POST /api/notes", auth(http.HandlerFunc(deps.CreateNote)))
	mux.Handle("PUT /api/notes/{title}/content", auth(http.HandlerFunc(deps.UpdateNoteContent)))
	mux.Handle("PUT /api/notes/{title}/meta", auth(http.HandlerFunc(deps.UpdateNoteMeta)))
	mux.Handle("PUT /api/notes/{title}/rename", auth(http.HandlerFunc(deps.RenameNote)))
	mux.Handle("DELETE /api/notes/{title}", auth(http.HandlerFunc(deps.DeleteNote)))
	mux.Handle("GET /api/notes/{title}/assets", auth(http.HandlerFunc(deps.ListMediaAssets)))
	mux.Handle("POST /api/notes/{title}/assets/reserve", auth(http.HandlerFunc(deps.ReserveMediaAsset)))
	mux.Handle("GET /api/notes/{title}/assets/{assetID}/status", auth(http.HandlerFunc(deps.GetMediaAsset)))
	mux.Handle("PUT /api/notes/{title}/assets/{assetID}/content", auth(http.HandlerFunc(deps.UploadMediaAsset)))
	mux.Handle("DELETE /api/notes/{title}/assets/{assetID}", auth(http.HandlerFunc(deps.AbandonMediaAsset)))
	mux.Handle("GET /api/trash", auth(http.HandlerFunc(deps.ListTrash)))
	mux.Handle("POST /api/trash/{id}/restore", auth(http.HandlerFunc(deps.RestoreTrash)))
	mux.Handle("DELETE /api/trash/{id}", auth(http.HandlerFunc(deps.PermanentlyDeleteTrash)))
	mux.Handle("DELETE /api/trash", auth(http.HandlerFunc(deps.EmptyTrash)))
	mux.Handle("GET /api/events", auth(http.HandlerFunc(deps.HandleSSE)))
	mux.Handle("POST /api/send", auth(http.HandlerFunc(deps.HandleSend)))

	if deps.AgentDeps != nil {
		mux.Handle("GET /api/agent/settings", auth(http.HandlerFunc(deps.AgentDeps.GetSettings)))
		mux.Handle("PUT /api/agent/settings", auth(http.HandlerFunc(deps.AgentDeps.UpdateSettings)))
		mux.Handle("GET /api/agent/personalization", auth(http.HandlerFunc(deps.AgentDeps.GetPersonalization)))
		mux.Handle("PUT /api/agent/personalization", auth(http.HandlerFunc(deps.AgentDeps.UpdatePersonalization)))
		mux.Handle("GET /api/agent/providers", auth(http.HandlerFunc(deps.AgentDeps.ListProviders)))
		mux.Handle("POST /api/agent/providers/{providerID}/connect/key", auth(http.HandlerFunc(deps.AgentDeps.ConnectProviderKey)))
		mux.Handle("POST /api/agent/providers/{providerID}/connect/oauth", auth(http.HandlerFunc(deps.AgentDeps.StartProviderOAuth)))
		mux.Handle("DELETE /api/agent/providers/{providerID}", auth(http.HandlerFunc(deps.AgentDeps.DisconnectProvider)))
		mux.Handle("GET /api/agent/provider-attempts/{attemptID}", auth(http.HandlerFunc(deps.AgentDeps.GetProviderOAuthAttempt)))
		mux.Handle("POST /api/agent/provider-attempts/{attemptID}/complete", auth(http.HandlerFunc(deps.AgentDeps.CompleteProviderOAuth)))
		mux.Handle("DELETE /api/agent/provider-attempts/{attemptID}", auth(http.HandlerFunc(deps.AgentDeps.CancelProviderOAuth)))
		mux.Handle("GET /api/agent/global/event", auth(http.HandlerFunc(deps.AgentDeps.ProxyGlobalSSE)))
		mux.Handle("GET /api/agent/{path...}", auth(http.HandlerFunc(deps.AgentDeps.ProxyJSON)))
		mux.Handle("POST /api/agent/{path...}", auth(http.HandlerFunc(deps.AgentDeps.ProxyJSON)))
		mux.Handle("PATCH /api/agent/{path...}", auth(http.HandlerFunc(deps.AgentDeps.ProxyJSON)))
		mux.Handle("PUT /api/agent/{path...}", auth(http.HandlerFunc(deps.AgentDeps.ProxyJSON)))
		mux.Handle("DELETE /api/agent/{path...}", auth(http.HandlerFunc(deps.AgentDeps.ProxyJSON)))
	}

	mux.Handle("GET /api/admin/requests", admin(http.HandlerFunc(deps.ListRequests)))
	mux.Handle("POST /api/admin/requests/{id}/approve", admin(http.HandlerFunc(deps.ApproveRequest)))
	mux.Handle("POST /api/admin/requests/{id}/reject", admin(http.HandlerFunc(deps.RejectRequest)))
	mux.Handle("GET /api/admin/devices", admin(http.HandlerFunc(deps.ListDevices)))
	mux.Handle("DELETE /api/admin/devices/{id}", admin(http.HandlerFunc(deps.RevokeDevice)))
}
