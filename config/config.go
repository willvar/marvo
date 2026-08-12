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
	"unicode/utf8"

	"marvo/internal/runtimeauth"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Log      LogConfig      `yaml:"log"`
	OpenCode OpenCodeConfig `yaml:"opencode"`
	Runtime  RuntimeConfig  `yaml:"runtime"`
}

type RuntimeConfig struct {
	URL       string `yaml:"url"`
	TokenFile string `yaml:"token_file"`
	Token     string `yaml:"-"`
}

type OpenCodeConfig struct {
	URL                    string `yaml:"url"`
	GlobalInstructionsFile string `yaml:"global_instructions_file"`
	LegacyHomeDir          string `yaml:"legacy_home_dir"`
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
	var err error
	c.Server.DataDir, err = expandHomePath(c.Server.DataDir, home, homeErr, "server.data_dir")
	if err != nil {
		return err
	}
	c.Server.DataDir, err = filepath.Abs(filepath.Clean(c.Server.DataDir))
	if err != nil {
		return fmt.Errorf("resolve server.data_dir: %w", err)
	}
	if c.Server.StateDir == "" {
		if filepath.Base(c.Server.DataDir) == "data" {
			c.Server.StateDir = filepath.Dir(c.Server.DataDir)
		} else {
			c.Server.StateDir = c.Server.DataDir + "-state"
		}
	}
	c.Server.StateDir, err = expandHomePath(c.Server.StateDir, home, homeErr, "server.state_dir")
	if err != nil {
		return err
	}
	c.Server.StateDir, err = filepath.Abs(filepath.Clean(c.Server.StateDir))
	if err != nil {
		return fmt.Errorf("resolve server.state_dir: %w", err)
	}
	if samePath(c.Server.StateDir, c.Server.DataDir) {
		return errors.New("server.state_dir must not be the legacy server.data_dir")
	}
	if c.Runtime.URL == "" {
		c.Runtime.URL = "http://127.0.0.1:4097"
	}
	if err := validateHTTPURL(c.Runtime.URL); err != nil {
		return fmt.Errorf("runtime.url: %w", err)
	}
	c.Runtime.URL = strings.TrimRight(c.Runtime.URL, "/")
	if c.Runtime.TokenFile == "" {
		c.Runtime.TokenFile = filepath.Join(c.Server.StateDir, "control", ".runtime-token")
	}
	c.Runtime.TokenFile, err = expandHomePath(c.Runtime.TokenFile, home, homeErr, "runtime.token_file")
	if err != nil {
		return err
	}
	c.Runtime.TokenFile, err = filepath.Abs(filepath.Clean(c.Runtime.TokenFile))
	if err != nil {
		return fmt.Errorf("resolve runtime.token_file: %w", err)
	}
	runtimeToken, err := runtimeauth.LoadOrCreateToken(c.Runtime.TokenFile)
	if err != nil {
		return fmt.Errorf("initialize runtime token: %w", err)
	}
	c.Runtime.Token = runtimeToken
	if c.OpenCode.LegacyHomeDir == "" {
		stateDir := strings.TrimSpace(os.Getenv("MARVO_OPENCODE_STATE_DIR"))
		if stateDir == "" {
			stateDir = "~/.marvo/opencode-state"
		}
		c.OpenCode.LegacyHomeDir = filepath.Join(stateDir, "home")
	}
	c.OpenCode.LegacyHomeDir, err = expandHomePath(c.OpenCode.LegacyHomeDir, home, homeErr, "opencode.legacy_home_dir")
	if err != nil {
		return err
	}
	c.OpenCode.LegacyHomeDir, err = filepath.Abs(filepath.Clean(c.OpenCode.LegacyHomeDir))
	if err != nil {
		return fmt.Errorf("resolve opencode.legacy_home_dir: %w", err)
	}
	if c.OpenCode.GlobalInstructionsFile == "" {
		c.OpenCode.GlobalInstructionsFile = filepath.Join(c.OpenCode.LegacyHomeDir, ".config", "opencode", "AGENTS.md")
	}
	c.OpenCode.GlobalInstructionsFile, err = expandHomePath(c.OpenCode.GlobalInstructionsFile, home, homeErr, "opencode.global_instructions_file")
	if err != nil {
		return err
	}
	c.OpenCode.GlobalInstructionsFile, err = filepath.Abs(filepath.Clean(c.OpenCode.GlobalInstructionsFile))
	if err != nil {
		return fmt.Errorf("resolve opencode.global_instructions_file: %w", err)
	}
	projectInstructions := filepath.Join(c.Server.DataDir, "AGENTS.md")
	if samePath(c.OpenCode.GlobalInstructionsFile, projectInstructions) {
		return errors.New("opencode.global_instructions_file must not overwrite the project AGENTS.md")
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	for _, origin := range c.Server.CORSOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("invalid server.cors_origins entry %q", origin)
		}
	}
	secret, err := loadOrCreateSessionSecret(c.Server.StateDir)
	if err != nil {
		return fmt.Errorf("initialize managed session secret: %w", err)
	}
	c.Server.SessionSecret = secret

	requiresStrongCredentials := !isLoopbackHost(c.Server.Host)
	for _, origin := range c.Server.CORSOrigins {
		parsed, _ := url.Parse(origin)
		if parsed != nil && !isLoopbackHost(parsed.Hostname()) {
			requiresStrongCredentials = true
		}
	}
	if !requiresStrongCredentials {
		return nil
	}
	password := strings.TrimSpace(c.Auth.Password)
	if utf8.RuneCountInString(password) < 12 || strings.EqualFold(password, "marvo") || strings.HasPrefix(password, "CHANGE_ME") {
		return errors.New("auth.password must be changed to a value of at least 12 characters for non-local access")
	}
	return nil
}

func expandHomePath(path, home string, homeErr error, field string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	if homeErr != nil || home == "" {
		return "", fmt.Errorf("cannot expand %s because the user home directory is unavailable", field)
	}
	return filepath.Join(home, path[2:]), nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func validateHTTPURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("must be an absolute http or https URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("cannot contain a query or fragment")
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

func loadOrCreateSessionSecret(stateDir string) (string, error) {
	controlDir := filepath.Join(stateDir, "control")
	if err := os.MkdirAll(controlDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(controlDir, ".session-secret")
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
	if err := writeNewPrivateFile(path, []byte(secret+"\n")); err != nil {
		return "", err
	}
	return secret, nil
}

func writeNewPrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".session-secret-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

type ServerConfig struct {
	Host          string   `yaml:"host"`
	Port          int      `yaml:"port"`
	StateDir      string   `yaml:"state_dir"`
	DataDir       string   `yaml:"data_dir"`
	SessionSecret string   `yaml:"-"`
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
