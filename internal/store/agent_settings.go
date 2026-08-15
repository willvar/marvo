package store

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
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
	state *StateDB
}

func NewAgentSettingsStore(state *StateDB) (*AgentSettingsStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	settingsStore := &AgentSettingsStore{state: state}
	if _, err := settingsStore.load(); err != nil {
		return nil, err
	}
	return settingsStore, nil
}

func (s *AgentSettingsStore) Get() AgentSettings {
	settings, err := s.load()
	if err != nil {
		slog.Error("failed to read Agent settings", "error", err)
		return AgentSettings{}
	}
	return settings
}

func (s *AgentSettingsStore) Save(settings AgentSettings) error {
	settings = copyAgentSettings(settings)
	settings.Variant = strings.TrimSpace(settings.Variant)
	if err := validateAgentSettings(settings); err != nil {
		return err
	}
	var provider any
	var model any
	if settings.Model != nil {
		provider = settings.Model.ProviderID
		model = settings.Model.ModelID
	}
	_, err := s.state.sql.Exec(`
		UPDATE space_settings
		SET agent_provider_id = ?, agent_model_id = ?, agent_variant = ?, agent_global_prompt = ?,
			updated_at = ?
		WHERE id = 1
	`, provider, model, settings.Variant, settings.GlobalPrompt, time.Now().UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("save Agent settings: %w", err)
	}
	return nil
}

func (s *AgentSettingsStore) load() (AgentSettings, error) {
	if s == nil || s.state == nil {
		return AgentSettings{}, errors.New("agent settings store is unavailable")
	}
	var provider *string
	var model *string
	var settings AgentSettings
	err := s.state.sql.QueryRow(`
		SELECT agent_provider_id, agent_model_id, agent_variant, agent_global_prompt
		FROM space_settings WHERE id = 1
	`).Scan(&provider, &model, &settings.Variant, &settings.GlobalPrompt)
	if err != nil {
		return AgentSettings{}, fmt.Errorf("load Agent settings: %w", err)
	}
	if provider != nil && model != nil {
		settings.Model = &AgentModelSelection{ProviderID: *provider, ModelID: *model}
	} else if provider != nil || model != nil {
		return AgentSettings{}, fmt.Errorf("%w: incomplete model selection", ErrInvalidAgentSettings)
	}
	if err := validateAgentSettings(settings); err != nil {
		return AgentSettings{}, err
	}
	return copyAgentSettings(settings), nil
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

func copyAgentSettings(settings AgentSettings) AgentSettings {
	copy := settings
	if settings.Model != nil {
		model := *settings.Model
		copy.Model = &model
	}
	return copy
}
