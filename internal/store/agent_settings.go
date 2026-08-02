package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	agentSettingsFilename = ".agent-settings.json"
	MaxGlobalPromptBytes  = 64 << 10
	maxAgentVariantBytes  = 256
	maxAgentSettingsBytes = 128 << 10
)

var ErrInvalidAgentSettings = errors.New("invalid Agent settings")

type AgentModelSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

type AgentSettings struct {
	Model        *AgentModelSelection `json:"model,omitempty"`
	Variant      string               `json:"variant"`
	GlobalPrompt string               `json:"global_prompt"`
}

type AgentSettingsStore struct {
	mu       sync.RWMutex
	path     string
	settings AgentSettings
}

func NewAgentSettingsStore(dataDir string) (*AgentSettingsStore, error) {
	settingsStore := &AgentSettingsStore{path: filepath.Join(dataDir, agentSettingsFilename)}
	if err := settingsStore.load(); err != nil {
		return nil, err
	}
	return settingsStore, nil
}

func (s *AgentSettingsStore) Get() AgentSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyAgentSettings(s.settings)
}

func (s *AgentSettingsStore) Save(settings AgentSettings) error {
	settings = copyAgentSettings(settings)
	settings.Variant = strings.TrimSpace(settings.Variant)
	if err := validateAgentSettings(settings); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRegularFileOrMissing(s.path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writePrivateFileAtomic(s.path, data); err != nil {
		return err
	}
	s.settings = settings
	return nil
}

func (s *AgentSettingsStore) load() error {
	if err := validateRegularFileOrMissing(s.path); err != nil {
		return err
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() > maxAgentSettingsBytes {
		return fmt.Errorf("%w: settings file is too large", ErrInvalidAgentSettings)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var settings AgentSettings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAgentSettings, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: settings file must contain one JSON value", ErrInvalidAgentSettings)
	}
	settings.Variant = strings.TrimSpace(settings.Variant)
	if err := validateAgentSettings(settings); err != nil {
		return err
	}
	s.settings = copyAgentSettings(settings)
	return nil
}

func validateAgentSettings(settings AgentSettings) error {
	if !utf8.ValidString(settings.GlobalPrompt) || len(settings.GlobalPrompt) > MaxGlobalPromptBytes {
		return fmt.Errorf("%w: global prompt must be valid UTF-8 and at most %d bytes", ErrInvalidAgentSettings, MaxGlobalPromptBytes)
	}
	if !utf8.ValidString(settings.Variant) || len(settings.Variant) > maxAgentVariantBytes {
		return fmt.Errorf("%w: model variant is invalid", ErrInvalidAgentSettings)
	}
	if settings.Model == nil {
		if settings.Variant != "" {
			return fmt.Errorf("%w: model variant requires a model", ErrInvalidAgentSettings)
		}
		return nil
	}
	providerID := strings.TrimSpace(settings.Model.ProviderID)
	modelID := strings.TrimSpace(settings.Model.ModelID)
	if providerID == "" || modelID == "" || len(providerID) > 256 || len(modelID) > 512 {
		return fmt.Errorf("%w: model selection is invalid", ErrInvalidAgentSettings)
	}
	settings.Model.ProviderID = providerID
	settings.Model.ModelID = modelID
	return nil
}

func validateRegularFileOrMissing(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: settings path is not a regular file", ErrInvalidAgentSettings)
	}
	return nil
}

func copyAgentSettings(settings AgentSettings) AgentSettings {
	copy := settings
	if settings.Model != nil {
		model := *settings.Model
		copy.Model = &model
	}
	return copy
}
