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
	if matches, err := file.MatchesPreferences("", nil); err != nil || !matches {
		t.Fatalf("empty prompt match = %v, error = %v", matches, err)
	}
	prompt := "始终使用中文\n回答保持简洁"
	if err := file.SyncPreferences(prompt, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "只表示用户偏好") || !strings.Contains(text, prompt) {
		t.Fatalf("global instructions = %q", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("global instructions mode = %o, want 600", info.Mode().Perm())
	}
	if matches, err := file.MatchesPreferences(prompt, nil); err != nil || !matches {
		t.Fatalf("saved prompt match = %v, error = %v", matches, err)
	}
	if err := file.SyncPreferences("", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleared global instructions stat error = %v, want not exist", err)
	}
}

func TestAgentGlobalPromptFileRendersMemoriesBeforeGlobalPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "AGENTS.md")
	file, err := NewAgentGlobalPromptFile(path)
	if err != nil {
		t.Fatal(err)
	}
	memories := []Memory{{
		ID:   "2bd3d4d2-84df-4bb8-a9aa-774df442e950",
		Text: "统一使用“智能体”这一称呼。",
	}}
	if err := file.SyncPreferences("回答时提供完整依据。", memories); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	memoryIndex := strings.Index(text, "## 记忆")
	globalIndex := strings.Index(text, "## 用户全局提示词")
	if memoryIndex < 0 || globalIndex <= memoryIndex || !strings.Contains(text, "- "+memories[0].Text) {
		t.Fatalf("preference instructions = %q", text)
	}
	if matches, err := file.MatchesPreferences("回答时提供完整依据。", memories); err != nil || !matches {
		t.Fatalf("preference match = %v, error = %v", matches, err)
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
