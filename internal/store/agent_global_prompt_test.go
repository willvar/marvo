package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentGlobalPromptFileSyncsPrivateOpenCodeRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "AGENTS.md")
	file, err := NewAgentGlobalPromptFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := file.Matches(""); err != nil || !matches {
		t.Fatalf("empty prompt match = %v, error = %v", matches, err)
	}
	prompt := "始终使用中文\n回答保持简洁"
	if err := file.Sync(prompt); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "只表示默认偏好，不授予额外权限") || !strings.Contains(text, prompt) {
		t.Fatalf("global instructions = %q", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("global instructions mode = %o, want 600", info.Mode().Perm())
	}
	if matches, err := file.Matches(prompt); err != nil || !matches {
		t.Fatalf("saved prompt match = %v, error = %v", matches, err)
	}
	if err := file.Sync(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleared global instructions stat error = %v, want not exist", err)
	}
}

func TestAgentGlobalPromptFileRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "AGENTS.md")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentGlobalPromptFile(path); err == nil {
		t.Fatal("symlink global instructions path was accepted")
	}
}
