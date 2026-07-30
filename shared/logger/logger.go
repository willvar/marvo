package logger

import (
	"log/slog"
	"os"
)

func Init(level string, filePath string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	if filePath != "" {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Error("failed to open log file", "error", err, "path", filePath)
			os.Exit(1)
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(f, opts)))
	} else {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
	}
}
