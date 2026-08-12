package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicCORSOriginRequiresStrongCredentialsBehindLoopbackProxy(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:          "127.0.0.1",
			StateDir:      filepath.Join(t.TempDir(), "state"),
			DataDir:       t.TempDir(),
			SessionSecret: "",
			CORSOrigins:   []string{"https://marvo.example.com"},
		},
		Auth:     AuthConfig{Password: "marvo"},
		OpenCode: OpenCodeConfig{URL: "http://127.0.0.1:4096"},
	}
	if err := cfg.resolve(); err == nil {
		t.Fatal("loopback backend with public browser origin accepted development credentials")
	}

	cfg.Server.SessionSecret = "0123456789abcdef0123456789abcdef"
	cfg.Auth.Password = "a strong admin password"
	if err := cfg.resolve(); err != nil {
		t.Fatalf("resolve() with production credentials error = %v", err)
	}
}

func TestLocalDevelopmentSecretIsCreatedPrivatelyAndReused(t *testing.T) {
	dataDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	newConfig := func() Config {
		return Config{
			Server: ServerConfig{Host: "127.0.0.1", StateDir: stateDir, DataDir: dataDir, CORSOrigins: []string{"http://localhost:5080"}},
			Auth:   AuthConfig{Password: "marvo"}, OpenCode: OpenCodeConfig{URL: "http://127.0.0.1:4096"},
		}
	}
	first := newConfig()
	if err := first.resolve(); err != nil {
		t.Fatalf("first resolve() error = %v", err)
	}
	if len(first.Server.SessionSecret) < 32 {
		t.Fatalf("generated secret is too short: %q", first.Server.SessionSecret)
	}
	path := filepath.Join(stateDir, "control", ".session-secret")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(.session-secret) error = %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf(".session-secret permissions = %04o, want 0600", info.Mode().Perm())
	}
	second := newConfig()
	if err := second.resolve(); err != nil {
		t.Fatalf("second resolve() error = %v", err)
	}
	if second.Server.SessionSecret != first.Server.SessionSecret {
		t.Fatal("local session secret changed across restart")
	}
}

func TestLegacyLocalSecretIsCopiedWithoutChangingIt(t *testing.T) {
	dataDir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	legacySecret := "legacy-session-secret-that-is-long-enough"
	if err := os.WriteFile(filepath.Join(dataDir, ".session-secret"), []byte(legacySecret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Server:   ServerConfig{Host: "127.0.0.1", StateDir: stateDir, DataDir: dataDir},
		Auth:     AuthConfig{Password: "marvo"},
		OpenCode: OpenCodeConfig{URL: "http://127.0.0.1:4096"},
	}
	if err := cfg.resolve(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.SessionSecret != legacySecret {
		t.Fatalf("session secret = %q", cfg.Server.SessionSecret)
	}
	if raw, err := os.ReadFile(filepath.Join(stateDir, "control", ".session-secret")); err != nil || strings.TrimSpace(string(raw)) != legacySecret {
		t.Fatalf("copied secret = %q, error = %v", raw, err)
	}
}

func TestOpenCodeGlobalInstructionsPathUsesStateDirectory(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("MARVO_OPENCODE_STATE_DIR", stateDir)
	cfg := Config{
		Server:   ServerConfig{Host: "127.0.0.1", StateDir: filepath.Join(t.TempDir(), "marvo"), DataDir: t.TempDir()},
		Auth:     AuthConfig{Password: "marvo"},
		OpenCode: OpenCodeConfig{URL: "http://127.0.0.1:4096"},
	}
	if err := cfg.resolve(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateDir, "home", ".config", "opencode", "AGENTS.md")
	if cfg.OpenCode.GlobalInstructionsFile != want {
		t.Fatalf("global instructions path = %q, want %q", cfg.OpenCode.GlobalInstructionsFile, want)
	}
}

func TestOpenCodeGlobalInstructionsCannotReplaceProjectRules(t *testing.T) {
	dataDir := t.TempDir()
	cfg := Config{
		Server: ServerConfig{Host: "127.0.0.1", StateDir: filepath.Join(t.TempDir(), "state"), DataDir: dataDir},
		Auth:   AuthConfig{Password: "marvo"},
		OpenCode: OpenCodeConfig{
			URL:                    "http://127.0.0.1:4096",
			GlobalInstructionsFile: filepath.Join(dataDir, "AGENTS.md"),
		},
	}
	if err := cfg.resolve(); err == nil {
		t.Fatal("project AGENTS.md accepted as the global instructions path")
	}
}
