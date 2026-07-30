package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"marvo/config"
	"marvo/internal/handler"
	"marvo/internal/search"
	"marvo/internal/store"
	"marvo/internal/ws"
	"marvo/shared/logger"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

const shutdownTimeout = 10 * time.Second

func Execute() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()

	cfg := config.Load(*configPath)
	logger.Init(cfg.Log.Level, cfg.Log.FilePath)

	if err := os.MkdirAll(cfg.Server.DataDir, 0755); err != nil {
		slog.Error("failed to create data directory", "error", err, "path", cfg.Server.DataDir)
		os.Exit(1)
	}

	noteStore := store.NewNoteStore(cfg.Server.DataDir)
	searchIdx, err := search.NewIndex(cfg.Server.DataDir)
	if err != nil {
		slog.Error("failed to initialize search index", "error", err)
		os.Exit(1)
	}

	hub := ws.NewHub()
	go hub.Run()

	w, err := store.WatchNotes(cfg.Server.DataDir, func(title string, content string) {
		doc := hub.OT.GetDocument(title)
		if doc != nil && doc.Content == content {
			return
		}
		doc = hub.OT.ResetDocument(title, content)
		searchIdx.IndexAsync(title, content, func(err error) {
			slog.Error("failed to reindex note after external change", "title", title, "error", err)
		})
		hub.BroadcastToNote(title, "", store.MustJSON(map[string]interface{}{
			"action":  "ot_snapshot",
			"title":   title,
			"content": content,
			"version": doc.Version,
		}))
	})
	if err != nil {
		slog.Error("failed to start file watcher", "error", err)
		os.Exit(1)
	}
	defer w.Close()

	app := fiber.New(fiber.Config{
		AppName:   "Marvo",
		BodyLimit: 50 * 1024 * 1024,
	})

	app.Use(recover.New())
	app.Use(compress.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     joinOrigins(cfg.Server.CORSOrigins),
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Content-Type",
		AllowCredentials: true,
	}))

	shuttingDown := make(chan struct{})

	handler.RegisterRoutes(app, &handler.Dependencies{
		Config:    cfg,
		NoteStore: noteStore,
		Search:    searchIdx,
		Hub:       hub,
		AIDeps:    handler.NewAIDeps(cfg.OpenCode.URL, shuttingDown),
	})
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		slog.Info("starting server", "addr", addr)
		if err := app.Listen(addr); err != nil {
			select {
			case <-shuttingDown:
				return
			default:
			}
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
	hub.Close()
	shutdownServer(app, shutdownTimeout)
	if err := w.Close(); err != nil {
		slog.Error("failed to close file watcher", "error", err)
	}
	closeSearchIndex(searchIdx, shutdownTimeout)
}

func shutdownServer(app *fiber.App, timeout time.Duration) {
	done := make(chan error, 1)
	go func() {
		done <- app.ShutdownWithTimeout(timeout)
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Error("failed to shut down server", "error", err)
		}
	case <-time.After(timeout + time.Second):
		slog.Warn("server shutdown timed out")
	}
}

func closeSearchIndex(searchIdx *search.Index, timeout time.Duration) {
	done := make(chan error, 1)
	go func() {
		done <- searchIdx.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Error("failed to close search index", "error", err)
		}
	case <-time.After(timeout):
		slog.Warn("search index close timed out")
	}
}

func joinOrigins(origins []string) string {
	result := ""
	for i, o := range origins {
		if i > 0 {
			result += ","
		}
		result += o
	}
	return result
}
