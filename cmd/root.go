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
	webapp "marvo/frontend"
	"marvo/internal/apprelease"
	"marvo/internal/control"
	"marvo/internal/handler"
	"marvo/internal/userspace"
	"marvo/shared/logger"
)

const shutdownTimeout = 10 * time.Second

func Execute() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()

	cfg := config.Load(*configPath)
	logger.Init(cfg.Log.Level, cfg.Log.FilePath)

	layout, err := userspace.OpenLayout(cfg.Server.StateDir)
	if err != nil {
		slog.Error("failed to initialize multi-user state layout", "error", err, "path", cfg.Server.StateDir)
		os.Exit(1)
	}
	controlDB, err := control.Open(layout.ControlDatabase(), cfg.Server.SessionSecret)
	if err != nil {
		slog.Error("failed to open platform control database", "error", err)
		os.Exit(1)
	}
	defer controlDB.Close()
	appReleases, err := apprelease.Open(layout.AndroidReleaseDirectory())
	if err != nil {
		if appReleases == nil {
			slog.Error("failed to initialize Android release store", "error", err)
			os.Exit(1)
		}
		slog.Warn("published Android release is unavailable; continuing without it", "error", err)
	}
	mux := http.NewServeMux()

	shuttingDown := make(chan struct{})
	spaces := handler.NewSpaceRegistry(cfg, controlDB, layout, shuttingDown)
	defer spaces.Close()
	spaces.StartRuntimeEvents()

	deps := &handler.Dependencies{
		Config:  cfg,
		Control: controlDB,
		Layout:  layout,
		Spaces:  spaces,
		Legacy: userspace.LegacySources{
			Workspace: cfg.Server.DataDir,
			AgentHome: cfg.OpenCode.LegacyHomeDir,
		},
		AppReleases: appReleases,
	}
	handler.RegisterRoutes(mux, deps)
	if frontend := webapp.Handler(); frontend != nil {
		mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		})
		mux.Handle("/", frontend)
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
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
