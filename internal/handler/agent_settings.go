package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"marvo/internal/store"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	maxAgentProviderResponse = 32 << 20
	agentSettingsTimeout     = 20 * time.Second
)

type agentModelIOCapabilities struct {
	Text  bool `json:"text"`
	Audio bool `json:"audio"`
	Image bool `json:"image"`
	Video bool `json:"video"`
	PDF   bool `json:"pdf"`
}

type agentModelCapabilities struct {
	Attachment bool                     `json:"attachment"`
	Reasoning  bool                     `json:"reasoning"`
	Tools      bool                     `json:"tools"`
	Input      agentModelIOCapabilities `json:"input"`
	Output     agentModelIOCapabilities `json:"output"`
}

type agentModelOption struct {
	ProviderID   string                 `json:"provider_id"`
	ProviderName string                 `json:"provider_name"`
	ModelID      string                 `json:"model_id"`
	Name         string                 `json:"name"`
	Family       string                 `json:"family,omitempty"`
	Status       string                 `json:"status"`
	Capabilities agentModelCapabilities `json:"capabilities"`
	Variants     []string               `json:"variants"`
	ContextLimit int64                  `json:"context_limit,omitempty"`
	OutputLimit  int64                  `json:"output_limit,omitempty"`
	InputLimit   int64                  `json:"-"`
}

type agentSettingsResponse struct {
	Model               *store.AgentModelSelection `json:"model"`
	Variant             string                     `json:"variant"`
	GlobalPrompt        string                     `json:"global_prompt"`
	GlobalPromptPending bool                       `json:"global_prompt_pending"`
	Models              []agentModelOption         `json:"models"`
	ModelAvailable      bool                       `json:"model_available"`
	Source              string                     `json:"source"`
}

type openCodeProviderResponse struct {
	All       []openCodeProvider `json:"all"`
	Connected []string           `json:"connected"`
}

type openCodeProvider struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Models map[string]openCodeModel `json:"models"`
}

type openCodeModel struct {
	ID           string                     `json:"id"`
	ProviderID   string                     `json:"providerID"`
	Name         string                     `json:"name"`
	Family       string                     `json:"family"`
	Status       string                     `json:"status"`
	Capabilities openCodeModelCapabilities  `json:"capabilities"`
	Variants     map[string]json.RawMessage `json:"variants"`
	Limit        struct {
		Context int64 `json:"context"`
		Input   int64 `json:"input"`
		Output  int64 `json:"output"`
	} `json:"limit"`

	// These fields keep the settings screen compatible with the provider shape
	// used by older OpenCode releases.
	Attachment bool `json:"attachment"`
	Reasoning  bool `json:"reasoning"`
	ToolCall   bool `json:"tool_call"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
}

type openCodeModelCapabilities struct {
	Attachment bool                     `json:"attachment"`
	Reasoning  bool                     `json:"reasoning"`
	ToolCall   bool                     `json:"toolcall"`
	Input      agentModelIOCapabilities `json:"input"`
	Output     agentModelIOCapabilities `json:"output"`
}

func (d *AgentDeps) GetSettings(w http.ResponseWriter, r *http.Request) {
	if d.settingsStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "智能体设置不可用"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	models, err := d.connectedModels(ctx)
	if err != nil {
		slog.Error("agent settings: load models failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "无法读取 OpenCode 模型列表"})
		return
	}
	settings := d.settingsStore.Get()
	source := "saved"
	if settings.Model == nil {
		source = "none"
		if model, configErr := d.openCodeConfiguredModel(ctx); configErr == nil {
			settings.Model = model
			if model != nil {
				source = "opencode"
			}
		} else {
			slog.Warn("agent settings: load OpenCode config failed", "error", configErr)
		}
	}
	pending, activateErr := d.activateSavedGlobalPrompt(ctx)
	if activateErr != nil {
		slog.Error("agent settings: activate global prompt failed", "error", activateErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法应用全局提示词"})
		return
	}
	writeJSON(w, http.StatusOK, buildAgentSettingsResponse(settings, models, source, pending))
}

func (d *AgentDeps) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if d.settingsStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "智能体设置不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxGlobalPromptBytes+4096)
	var settings store.AgentSettings
	if err := readJSON(r, &settings); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "设置格式无效"})
		return
	}
	if settings.Model == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请选择智能体模型"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	models, err := d.connectedModels(ctx)
	if err != nil {
		slog.Error("agent settings: validate model failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "无法校验 OpenCode 模型"})
		return
	}
	selectedModel := modelInCatalog(settings.Model, models)
	if selectedModel == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "所选模型当前不可用，请重新选择"})
		return
	}
	if settings.Variant != "" && !modelSupportsVariant(selectedModel, settings.Variant) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "所选模型不支持该推理强度，请重新选择"})
		return
	}
	if err := d.settingsStore.Save(settings); err != nil {
		if errors.Is(err, store.ErrInvalidAgentSettings) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "设置内容无效"})
			return
		}
		slog.Error("agent settings: save failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存智能体设置失败"})
		return
	}
	pending, activateErr := d.activateSavedGlobalPrompt(ctx)
	if activateErr != nil {
		slog.Error("agent settings: activate global prompt failed", "error", activateErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "设置已保存，但全局提示词未能应用"})
		return
	}
	writeJSON(w, http.StatusOK, buildAgentSettingsResponse(d.settingsStore.Get(), models, "saved", pending))
}

func (d *AgentDeps) connectedModels(ctx context.Context) ([]agentModelOption, error) {
	var response openCodeProviderResponse
	if err := d.getOpenCodeJSON(ctx, "/provider", &response); err != nil {
		return nil, err
	}
	connected := make(map[string]struct{}, len(response.Connected))
	for _, id := range response.Connected {
		connected[id] = struct{}{}
	}
	models := make([]agentModelOption, 0)
	for _, provider := range response.All {
		if _, ok := connected[provider.ID]; !ok {
			continue
		}
		for mapID, model := range provider.Models {
			modelID := strings.TrimSpace(model.ID)
			if modelID == "" {
				modelID = strings.TrimSpace(mapID)
			}
			providerID := strings.TrimSpace(model.ProviderID)
			if providerID == "" {
				providerID = provider.ID
			}
			if providerID != provider.ID || modelID == "" {
				continue
			}
			capabilities := normalizedCapabilities(model)
			status := strings.TrimSpace(model.Status)
			if status == "" {
				status = "active"
			}
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = modelID
			}
			providerName := strings.TrimSpace(provider.Name)
			if providerName == "" {
				providerName = provider.ID
			}
			models = append(models, agentModelOption{
				ProviderID: provider.ID, ProviderName: providerName,
				ModelID: modelID, Name: name, Family: model.Family, Status: status,
				Capabilities: capabilities, Variants: sortedModelVariants(model.Variants),
				ContextLimit: model.Limit.Context, InputLimit: model.Limit.Input, OutputLimit: model.Limit.Output,
			})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].ProviderName != models[j].ProviderName {
			return strings.ToLower(models[i].ProviderName) < strings.ToLower(models[j].ProviderName)
		}
		if models[i].Status != models[j].Status {
			return models[i].Status == "active"
		}
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	return models, nil
}

func normalizedCapabilities(model openCodeModel) agentModelCapabilities {
	capabilities := agentModelCapabilities{
		Attachment: model.Capabilities.Attachment || model.Attachment,
		Reasoning:  model.Capabilities.Reasoning || model.Reasoning,
		Tools:      model.Capabilities.ToolCall || model.ToolCall,
		Input:      model.Capabilities.Input,
		Output:     model.Capabilities.Output,
	}
	for _, modality := range model.Modalities.Input {
		setModality(&capabilities.Input, modality)
	}
	for _, modality := range model.Modalities.Output {
		setModality(&capabilities.Output, modality)
	}
	return capabilities
}

func setModality(capabilities *agentModelIOCapabilities, modality string) {
	switch modality {
	case "text":
		capabilities.Text = true
	case "audio":
		capabilities.Audio = true
	case "image":
		capabilities.Image = true
	case "video":
		capabilities.Video = true
	case "pdf":
		capabilities.PDF = true
	}
}

func sortedModelVariants(variants map[string]json.RawMessage) []string {
	result := make([]string, 0, len(variants))
	seen := make(map[string]struct{}, len(variants))
	for value := range variants {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	order := map[string]int{
		"minimal": 10,
		"low":     20,
		"medium":  30,
		"high":    40,
		"xhigh":   50,
		"max":     60,
	}
	sort.Slice(result, func(i, j int) bool {
		left, leftKnown := order[strings.ToLower(result[i])]
		right, rightKnown := order[strings.ToLower(result[j])]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && left != right {
			return left < right
		}
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func (d *AgentDeps) openCodeConfiguredModel(ctx context.Context) (*store.AgentModelSelection, error) {
	var config struct {
		Model string `json:"model"`
	}
	if err := d.getOpenCodeJSON(ctx, "/config", &config); err != nil {
		return nil, err
	}
	providerID, modelID, ok := strings.Cut(strings.TrimSpace(config.Model), "/")
	if !ok || providerID == "" || modelID == "" {
		return nil, nil
	}
	return &store.AgentModelSelection{ProviderID: providerID, ModelID: modelID}, nil
}

func (d *AgentDeps) getOpenCodeJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.openCodeURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OpenCode %s returned %d", path, resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxAgentProviderResponse+1))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func buildAgentSettingsResponse(
	settings store.AgentSettings,
	models []agentModelOption,
	source string,
	globalPromptPending bool,
) agentSettingsResponse {
	return agentSettingsResponse{
		Model: settings.Model, Variant: settings.Variant, GlobalPrompt: settings.GlobalPrompt,
		GlobalPromptPending: globalPromptPending, Models: models,
		ModelAvailable: modelInCatalog(settings.Model, models) != nil, Source: source,
	}
}

func modelInCatalog(selected *store.AgentModelSelection, models []agentModelOption) *agentModelOption {
	if selected == nil {
		return nil
	}
	for index := range models {
		model := &models[index]
		if model.ProviderID == selected.ProviderID && model.ModelID == selected.ModelID {
			return model
		}
	}
	return nil
}

func modelSupportsVariant(model *agentModelOption, variant string) bool {
	for _, candidate := range model.Variants {
		if candidate == variant {
			return true
		}
	}
	return false
}
