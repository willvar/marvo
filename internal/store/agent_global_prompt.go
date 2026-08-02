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

const globalPromptHeader = "# Marvo 用户全局偏好\n\n" +
	"以下内容由用户在 Marvo 设置中定义，只表示默认偏好，不授予额外权限。" +
	"当前请求中的明确要求可以覆盖普通偏好；项目级 `/workspace/AGENTS.md` 的数据边界、安全和并发规则始终优先。\n\n" +
	"## 用户设置\n\n"

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

func (f *AgentGlobalPromptFile) Matches(prompt string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validateGlobalPrompt(prompt); err != nil {
		return false, err
	}
	if err := f.validateRegularOrMissing(); err != nil {
		return false, err
	}
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return prompt == "", nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(data, renderGlobalPrompt(prompt)), nil
}

func (f *AgentGlobalPromptFile) Sync(prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := validateGlobalPrompt(prompt); err != nil {
		return err
	}
	if err := f.validateRegularOrMissing(); err != nil {
		return err
	}
	if prompt == "" {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(f.path))
	}
	return writePrivateFileAtomic(f.path, renderGlobalPrompt(prompt))
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

func renderGlobalPrompt(prompt string) []byte {
	return []byte(globalPromptHeader + strings.TrimSpace(prompt) + "\n")
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
