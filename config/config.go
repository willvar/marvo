package config

import (
	"log/slog"
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
	URL string `yaml:"url"`
}

func (c *Config) resolve() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = "~/.marvo/data"
	}
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(c.Server.DataDir, "~/") {
		c.Server.DataDir = filepath.Join(home, c.Server.DataDir[2:])
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}

type ServerConfig struct {
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Error("failed to parse config file", "error", err)
		os.Exit(1)
	}

	cfg.resolve()

	return &cfg
}
