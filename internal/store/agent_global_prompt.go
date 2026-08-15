package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const userPreferencesHeader = "# Marvo 用户偏好\n\n" +
	"以下内容只表示用户偏好，不改变 Marvo 的工作范围和应用规则。" +
	"当前请求中的明确要求可以覆盖普通偏好；项目级 `/workspace/AGENTS.md` 始终优先。\n\n"

type AgentGlobalPromptFile struct {
	mu   sync.Mutex
	path string
}

func NewAgentGlobalPromptFile(path string) (*AgentGlobalPromptFile, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, errors.New("global prompt path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file := &AgentGlobalPromptFile{path: path}
	if err := file.validateRegularOrMissing(); err != nil {
		return nil, err
	}
	return file, nil
}

func (f *AgentGlobalPromptFile) MatchesPreferences(prompt string, memories []Memory) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validateGlobalPrompt(prompt); err != nil {
		return false, err
	}
	if _, err := normalizeMemories(memories, false); err != nil {
		return false, err
	}
	if err := f.validateRegularOrMissing(); err != nil {
		return false, err
	}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return prompt == "" && len(memories) == 0, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(data, renderGlobalPrompt(prompt, memories)), nil
}

func (f *AgentGlobalPromptFile) SyncPreferences(prompt string, memories []Memory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validateGlobalPrompt(prompt); err != nil {
		return err
	}
	if _, err := normalizeMemories(memories, false); err != nil {
		return err
	}
	if err := f.validateRegularOrMissing(); err != nil {
		return err
	}
	if prompt == "" && len(memories) == 0 {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(f.path))
	}
	return writePrivateFileAtomic(f.path, renderGlobalPrompt(prompt, memories))
}

func (f *AgentGlobalPromptFile) validateRegularOrMissing() error {
	info, err := os.Lstat(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("global prompt path is not a regular file")
	}
	return nil
}

func validateGlobalPrompt(prompt string) error {
	if !utf8.ValidString(prompt) || len(prompt) > MaxGlobalPromptBytes {
		return fmt.Errorf("%w: global prompt must be valid UTF-8 and at most %d bytes", ErrInvalidAgentSettings, MaxGlobalPromptBytes)
	}
	return nil
}

func renderGlobalPrompt(prompt string, memories []Memory) []byte {
	var result strings.Builder
	result.WriteString(userPreferencesHeader)
	if len(memories) > 0 {
		result.WriteString("## 记忆\n\n")
		result.WriteString("这些记忆由用户和智能体共同维护，属于低于当前请求和用户全局提示词的默认偏好。\n\n")
		for _, memory := range memories {
			result.WriteString("- ")
			result.WriteString(strings.TrimSpace(memory.Text))
			result.WriteByte('\n')
		}
		result.WriteByte('\n')
	}
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		result.WriteString("## 用户全局提示词\n\n")
		result.WriteString(prompt)
		result.WriteByte('\n')
	}
	return []byte(result.String())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
