package cmd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"marvo/config"
	"marvo/internal/collab"
	"marvo/internal/handler"
	"marvo/internal/media"
	"marvo/internal/store"
	"marvo/shared/logger"
)

const shutdownTimeout = 10 * time.Second

func Execute() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()

	cfg := config.Load(*configPath)
	logger.Init(cfg.Log.Level, cfg.Log.FilePath)

	if err := os.MkdirAll(cfg.Server.DataDir, 0700); err != nil {
		slog.Error("failed to create data directory", "error", err, "path", cfg.Server.DataDir)
		os.Exit(1)
	}
	if err := handler.EnsureThemeFile(cfg.Server.DataDir); err != nil {
		slog.Error("failed to initialize theme file", "error", err)
		os.Exit(1)
	}

	noteStore := store.NewNoteStore(cfg.Server.DataDir)
	mediaManager := media.NewManager(noteStore)
	agentSettingsStore, err := store.NewAgentSettingsStore(cfg.Server.DataDir)
	if err != nil {
		slog.Error("failed to load Agent settings", "error", err)
		os.Exit(1)
	}
	agentPersonalizationStore, err := store.NewAgentPersonalizationStore(cfg.Server.DataDir)
	if err != nil {
		slog.Error("failed to load Agent personalization", "error", err)
		os.Exit(1)
	}
	agentGlobalPromptFile, err := store.NewAgentGlobalPromptFile(cfg.OpenCode.GlobalInstructionsFile)
	if err != nil {
		slog.Error("failed to initialize Agent global prompt file", "error", err, "path", cfg.OpenCode.GlobalInstructionsFile)
		os.Exit(1)
	}

	hub := collab.NewHub()
	mediaManager.SetChangeHandler(func(title string, asset media.Asset) {
		hub.BroadcastToNote(title, "", store.MustJSON(map[string]any{
			"action": "asset_changed",
			"title":  title,
			"asset":  asset,
		}))
	})

	w, err := store.WatchNotes(cfg.Server.DataDir, func(title string) {
		snapshot, snapshotErr := noteStore.Snapshot(title)
		if snapshotErr != nil {
			return
		}
		mediaManager.ReconcileNote(title, snapshot.InstanceToken)
		hub.BroadcastToNote(title, "", store.MustJSON(map[string]any{
			"action":           "note_changed",
			"title":            title,
			"note":             snapshot.Note,
			"content":          snapshot.Content,
			"content_revision": snapshot.ContentRevision,
			"meta_revision":    snapshot.MetaRevision,
			"instance_token":   snapshot.InstanceToken,
		}))
	}, func() {
		hub.BroadcastAll(store.MustJSON(map[string]any{
			"action": "note_list_changed",
		}))
	}, func() {
		hub.BroadcastAll(store.MustJSON(map[string]any{
			"action": "theme_changed",
		}))
	})
	if err != nil {
		slog.Error("failed to start file watcher", "error", err)
		os.Exit(1)
	}
	defer w.Close()

	mux := http.NewServeMux()

	shuttingDown := make(chan struct{})

	deps := &handler.Dependencies{
		Config:    cfg,
		NoteStore: noteStore,
		Hub:       hub,
		Media:     mediaManager,
		AgentDeps: handler.NewAgentDeps(
			cfg.OpenCode.URL,
			shuttingDown,
			agentSettingsStore,
			agentPersonalizationStore,
			agentGlobalPromptFile,
		),
		DeviceStore: store.NewDeviceStore(cfg.Server.DataDir, cfg.Server.SessionSecret),
	}
	handler.RegisterRoutes(mux, deps)

	var app http.Handler = mux
	app = corsMiddleware(cfg.Server.CORSOrigins)(app)
	app = originGuardMiddleware(cfg.Server.CORSOrigins)(app)
	app = securityHeadersMiddleware(app)
	app = recoveryMiddleware(app)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		slog.Info("starting server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	signal.Stop(quit)

	slog.Info("shutting down...")
	close(shuttingDown)
	mediaManager.Close()
	hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	if err := w.Close(); err != nil {
		slog.Error("failed to close file watcher", "error", err)
	}
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")

			if r.Method == http.MethodOptions {
				if origin != "" && originAllowed(origin, allowedOrigins) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Marvo-Instance-Token")
					w.Header().Set("Access-Control-Max-Age", "600")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if origin != "" && originAllowed(origin, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func originGuardMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") && !originAllowed(origin, allowedOrigins) {
				http.Error(w, "cross-site request rejected", http.StatusForbidden)
				return
			}
			if origin != "" && !originAllowed(origin, allowedOrigins) && !sameRequestOrigin(origin, r) {
				http.Error(w, "origin rejected", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameRequestOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string, allowedOrigins []string) bool {
	for _, o := range allowedOrigins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "error", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
