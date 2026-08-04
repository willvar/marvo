package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Log      LogConfig      `yaml:"log"`
	OpenCode OpenCodeConfig `yaml:"opencode"`
}

type OpenCodeConfig struct {
	URL                    string `yaml:"url"`
	GlobalInstructionsFile string `yaml:"global_instructions_file"`
}

func (c *Config) resolve() error {
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 5090
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = "~/.marvo/data"
	}
	home, homeErr := os.UserHomeDir()
	if strings.HasPrefix(c.Server.DataDir, "~/") {
		if homeErr != nil || home == "" {
			return errors.New("cannot expand server.data_dir because the user home directory is unavailable")
		}
		c.Server.DataDir = filepath.Join(home, c.Server.DataDir[2:])
	}
	c.Server.DataDir = filepath.Clean(c.Server.DataDir)
	if c.OpenCode.GlobalInstructionsFile == "" {
		stateDir := strings.TrimSpace(os.Getenv("MARVO_OPENCODE_STATE_DIR"))
		if stateDir == "" {
			stateDir = "~/.marvo/opencode-state"
		}
		c.OpenCode.GlobalInstructionsFile = filepath.Join(stateDir, "home", ".config", "opencode", "AGENTS.md")
	}
	if strings.HasPrefix(c.OpenCode.GlobalInstructionsFile, "~/") {
		if homeErr != nil || home == "" {
			return errors.New("cannot expand opencode.global_instructions_file because the user home directory is unavailable")
		}
		c.OpenCode.GlobalInstructionsFile = filepath.Join(home, c.OpenCode.GlobalInstructionsFile[2:])
	}
	c.OpenCode.GlobalInstructionsFile = filepath.Clean(c.OpenCode.GlobalInstructionsFile)
	projectInstructions := filepath.Join(c.Server.DataDir, "AGENTS.md")
	if samePath(c.OpenCode.GlobalInstructionsFile, projectInstructions) {
		return errors.New("opencode.global_instructions_file must not overwrite the project AGENTS.md")
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if err := validateOpenCodeURL(c.OpenCode.URL); err != nil {
		return err
	}
	for _, origin := range c.Server.CORSOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid server.cors_origins entry %q", origin)
		}
	}

	requiresStrongCredentials := !isLoopbackHost(c.Server.Host)
	for _, origin := range c.Server.CORSOrigins {
		parsed, _ := url.Parse(origin)
		if parsed != nil && !isLoopbackHost(parsed.Hostname()) {
			requiresStrongCredentials = true
		}
	}
	if !requiresStrongCredentials {
		if c.Server.SessionSecret == "" {
			secret, err := loadOrCreateLocalSecret(c.Server.DataDir)
			if err != nil {
				return fmt.Errorf("initialize local session secret: %w", err)
			}
			c.Server.SessionSecret = secret
		}
		return nil
	}
	if len(c.Server.SessionSecret) < 32 || strings.HasPrefix(c.Server.SessionSecret, "CHANGE_ME") {
		return errors.New("server.session_secret must be an explicit random value of at least 32 characters for non-local access")
	}
	password := strings.TrimSpace(c.Auth.Password)
	if len(password) < 12 || strings.EqualFold(password, "marvo") || strings.HasPrefix(password, "CHANGE_ME") {
		return errors.New("auth.password must be changed to a value of at least 12 characters for non-local access")
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func validateOpenCodeURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("opencode.url must be an absolute http or https URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("opencode.url cannot contain a query or fragment")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loadOrCreateLocalSecret(dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, ".session-secret")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", errors.New(".session-secret is not a regular file")
		}
		raw, readErr := os.ReadFile(path)
		secret := strings.TrimSpace(string(raw))
		if readErr != nil || len(secret) < 32 {
			return "", errors.New(".session-secret is unreadable or invalid")
		}
		return secret, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)
	tmp, err := os.CreateTemp(dataDir, ".session-secret-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return "", err
	}
	if _, err := tmp.WriteString(secret + "\n"); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return secret, nil
}

type ServerConfig struct {
	Host          string   `yaml:"host"`
	Port          int      `yaml:"port"`
	DataDir       string   `yaml:"data_dir"`
	SessionSecret string   `yaml:"session_secret"`
	CORSOrigins   []string `yaml:"cors_origins"`
}

type AuthConfig struct {
	Password string `yaml:"password"`
}

type LogConfig struct {
	Level    string `yaml:"level"`
	FilePath string `yaml:"file"`
}

func Load(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Error("failed to read config file", "error", err, "path", path)
		os.Exit(1)
	}

	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		slog.Error("failed to parse config file", "error", err)
		os.Exit(1)
	}

	if err := cfg.resolve(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	return &cfg
}
