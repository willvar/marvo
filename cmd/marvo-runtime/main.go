package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"marvo/internal/runtimegateway"
)

func main() {
	config, err := runtimegateway.ConfigFromEnvironment()
	if err != nil {
		slog.Error("invalid runtime gateway configuration", "error", err)
		os.Exit(1)
	}
	dockerClient := runtimegateway.NewDockerClient(config.DockerSocket)
	manager := runtimegateway.NewRuntimeManager(config, dockerClient)
	startupContext, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	if err := manager.Validate(startupContext); err != nil {
		cancelStartup()
		slog.Error("runtime gateway startup failed", "error", err)
		os.Exit(1)
	}
	cancelStartup()
	reaperContext, stopReaper := context.WithCancel(context.Background())
	defer stopReaper()
	go manager.RunIdleReaper(reaperContext)

	server := &http.Server{
		Addr: config.ListenAddress, Handler: runtimegateway.NewServer(config.Token, manager).Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20,
	}
	go func() {
		slog.Info("runtime gateway started", "addr", config.ListenAddress, "network", config.Network)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("runtime gateway stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)
	stopReaper()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("runtime gateway shutdown failed", "error", err)
	}
}
