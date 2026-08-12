package runtimegateway

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"marvo/internal/runtimeauth"
)

type Config struct {
	ListenAddress    string
	TokenFile        string
	Token            string
	DockerSocket     string
	Network          string
	AgentImage       string
	AgentGeneration  string
	StateDir         string
	HostStateDir     string
	RuntimeUID       int
	RuntimeGID       int
	MemoryBytes      int64
	NanoCPUs         int64
	PidsLimit        int64
	ReadinessTimeout time.Duration
	IdleTimeout      time.Duration
}

func ConfigFromEnvironment() (Config, error) {
	config := Config{
		ListenAddress:    envOr("MARVO_RUNTIME_LISTEN", "0.0.0.0:4097"),
		TokenFile:        envOr("MARVO_RUNTIME_TOKEN_FILE", "/state/control/.runtime-token"),
		DockerSocket:     envOr("MARVO_RUNTIME_DOCKER_SOCKET", "/var/run/docker.sock"),
		Network:          envOr("MARVO_RUNTIME_NETWORK", "marvo-runtime"),
		AgentImage:       envOr("MARVO_AGENT_IMAGE", "marvo-opencode:local"),
		AgentGeneration:  strings.TrimSpace(os.Getenv("MARVO_AGENT_GENERATION")),
		StateDir:         envOr("MARVO_RUNTIME_STATE_DIR", "/state"),
		HostStateDir:     strings.TrimSpace(os.Getenv("MARVO_RUNTIME_HOST_STATE_DIR")),
		MemoryBytes:      4 << 30,
		NanoCPUs:         4_000_000_000,
		PidsLimit:        1024,
		ReadinessTimeout: 60 * time.Second,
		IdleTimeout:      30 * time.Minute,
	}
	var err error
	if config.RuntimeUID, err = envInt("MARVO_RUNTIME_UID", -1); err != nil {
		return Config{}, err
	}
	if config.RuntimeGID, err = envInt("MARVO_RUNTIME_GID", -1); err != nil {
		return Config{}, err
	}
	if config.MemoryBytes, err = envInt64("MARVO_AGENT_MEMORY_BYTES", config.MemoryBytes); err != nil {
		return Config{}, err
	}
	if config.NanoCPUs, err = envInt64("MARVO_AGENT_NANO_CPUS", config.NanoCPUs); err != nil {
		return Config{}, err
	}
	if config.PidsLimit, err = envInt64("MARVO_AGENT_PIDS_LIMIT", config.PidsLimit); err != nil {
		return Config{}, err
	}
	readinessSeconds, err := envInt("MARVO_AGENT_READY_TIMEOUT", int(config.ReadinessTimeout/time.Second))
	if err != nil || readinessSeconds < 1 || readinessSeconds > 600 {
		return Config{}, errors.New("MARVO_AGENT_READY_TIMEOUT must be between 1 and 600 seconds")
	}
	config.ReadinessTimeout = time.Duration(readinessSeconds) * time.Second
	idleSeconds, err := envInt("MARVO_AGENT_IDLE_TIMEOUT", int(config.IdleTimeout/time.Second))
	if err != nil || idleSeconds < 0 || idleSeconds > 7*24*60*60 {
		return Config{}, errors.New("MARVO_AGENT_IDLE_TIMEOUT must be between 0 and 604800 seconds")
	}
	config.IdleTimeout = time.Duration(idleSeconds) * time.Second

	if !filepath.IsAbs(config.StateDir) {
		return Config{}, errors.New("MARVO_RUNTIME_STATE_DIR must be an absolute container path")
	}
	config.StateDir = filepath.Clean(config.StateDir)
	if config.HostStateDir == "" || !filepath.IsAbs(config.HostStateDir) {
		return Config{}, errors.New("MARVO_RUNTIME_HOST_STATE_DIR must be an absolute host path")
	}
	config.HostStateDir = filepath.Clean(config.HostStateDir)
	if config.RuntimeUID < 0 || config.RuntimeGID < 0 {
		return Config{}, errors.New("MARVO_RUNTIME_UID and MARVO_RUNTIME_GID are required non-negative integers")
	}
	if config.Network == "" || config.AgentImage == "" || strings.ContainsAny(config.Network, "\r\n") || strings.ContainsAny(config.AgentImage, "\r\n") {
		return Config{}, errors.New("runtime network and Agent image are required")
	}
	if len(config.AgentGeneration) > 512 || strings.ContainsAny(config.AgentGeneration, "\r\n") {
		return Config{}, errors.New("agent generation is invalid")
	}
	if config.MemoryBytes < 0 || config.NanoCPUs < 0 || config.PidsLimit < 0 {
		return Config{}, errors.New("runtime resource limits cannot be negative")
	}
	config.Token, err = runtimeauth.LoadOrCreateToken(config.TokenFile)
	if err != nil {
		return Config{}, fmt.Errorf("load runtime token: %w", err)
	}
	return config, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
