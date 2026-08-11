package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxAgentProviderRequest = 72 << 10
	maxAgentProviderKey     = 64 << 10
	maxAgentProviderInput   = 4096
	providerOAuthLifetime   = 10 * time.Minute
	providerAttemptKeep     = 10 * time.Minute
)

var providerCodePattern = regexp.MustCompile(`(?i)\bcode(?:\s+is\s+|\s*[:：]\s*)([A-Z0-9][A-Z0-9-]{2,31})\b`)

type openCodeProviderAuthMethod struct {
	Type    string                   `json:"type"`
	Label   string                   `json:"label"`
	Prompts []openCodeProviderPrompt `json:"prompts"`
}

type openCodeProviderPrompt struct {
	Type        string                   `json:"type"`
	Key         string                   `json:"key"`
	Message     string                   `json:"message"`
	Placeholder string                   `json:"placeholder"`
	Options     []openCodeProviderOption `json:"options"`
	When        *openCodeProviderWhen    `json:"when"`
}

type openCodeProviderOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

type openCodeProviderWhen struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type agentProviderMethod struct {
	Index             int                      `json:"index"`
	Type              string                   `json:"type"`
	Label             string                   `json:"label"`
	Prompts           []openCodeProviderPrompt `json:"prompts"`
	Available         bool                     `json:"available"`
	UnavailableReason string                   `json:"unavailable_reason,omitempty"`
}

type agentProviderOption struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Source        string                `json:"source"`
	Connected     bool                  `json:"connected"`
	CanDisconnect bool                  `json:"can_disconnect"`
	ModelCount    int                   `json:"model_count"`
	Methods       []agentProviderMethod `json:"methods"`
	CredentialIDs []string              `json:"-"`
	LegacyAuth    bool                  `json:"-"`
}

type openCodeIntegrationListResponse struct {
	Data []openCodeIntegration `json:"data"`
}

type openCodeIntegration struct {
	ID          string                          `json:"id"`
	Connections []openCodeIntegrationConnection `json:"connections"`
}

type openCodeIntegrationConnection struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type agentProviderKeyRequest struct {
	MethodIndex int               `json:"method_index"`
	Key         string            `json:"key"`
	Inputs      map[string]string `json:"inputs"`
}

type agentProviderOAuthRequest struct {
	MethodIndex int               `json:"method_index"`
	Inputs      map[string]string `json:"inputs"`
}

type openCodeOAuthAuthorization struct {
	URL          string `json:"url"`
	Method       string `json:"method"`
	Instructions string `json:"instructions"`
}

type agentProviderOAuthAttempt struct {
	ID           string
	ProviderID   string
	ProviderName string
	MethodIndex  int
	MethodLabel  string
	Mode         string
	URL          string
	Instructions string
	Code         string
	Status       string
	Error        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	CompletedAt  time.Time
	Completing   bool
	Context      context.Context
	Cancel       context.CancelFunc
}

type agentProviderOAuthAttemptResponse struct {
	ID           string `json:"id"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	MethodLabel  string `json:"method_label"`
	Mode         string `json:"mode"`
	URL          string `json:"url"`
	Instructions string `json:"instructions"`
	Code         string `json:"code,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at"`
}

func (d *AgentDeps) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	providers, err := d.providerCatalog(ctx)
	if err != nil {
		slog.Error("agent providers: load catalog failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "无法读取 OpenCode 提供商列表"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (d *AgentDeps) ConnectProviderKey(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.PathValue("providerID"))
	var input agentProviderKeyRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentProviderRequest)
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接信息格式无效"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	provider, method, err := d.resolveProviderMethod(ctx, providerID, input.MethodIndex, "api")
	if err != nil {
		writeProviderValidationError(w, err)
		return
	}
	key := strings.TrimSpace(input.Key)
	if key == "" || len(key) > maxAgentProviderKey || !utf8.ValidString(key) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请输入有效的 API Key"})
		return
	}
	inputs, err := validatedProviderInputs(method.Prompts, input.Inputs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	opID, err := d.reserveProviderOperation(provider.ID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该提供商已有连接操作正在进行", "code": "agent_provider_busy"})
		return
	}
	defer d.releaseProviderOperation(provider.ID, opID)

	payload := map[string]any{"type": "api", "key": key}
	if len(inputs) > 0 {
		payload["metadata"] = inputs
	}
	if err := d.doOpenCodeJSON(ctx, http.MethodPut, "/auth/"+url.PathEscape(provider.ID), payload, nil); err != nil {
		slog.Error("agent providers: API key connect failed", "provider", provider.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 未能保存该连接"})
		return
	}
	if err := d.disposeOpenCode(ctx); err != nil {
		slog.Error("agent providers: refresh after API key connect failed", "provider", provider.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "连接已保存，但 OpenCode 刷新失败，请重试"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *AgentDeps) StartProviderOAuth(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.PathValue("providerID"))
	var input agentProviderOAuthRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentProviderRequest)
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "授权信息格式无效"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	provider, method, err := d.resolveProviderMethod(ctx, providerID, input.MethodIndex, "oauth")
	if err != nil {
		writeProviderValidationError(w, err)
		return
	}
	if !method.Available {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": method.UnavailableReason})
		return
	}
	inputs, err := validatedProviderInputs(method.Prompts, input.Inputs)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	attemptID, err := randomProviderAttemptID()
	if err != nil {
		slog.Error("agent providers: create OAuth attempt ID failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法启动授权"})
		return
	}
	if err := d.reserveProviderOperationWithID(provider.ID, attemptID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该提供商已有连接操作正在进行", "code": "agent_provider_busy"})
		return
	}

	payload := map[string]any{"method": method.Index}
	if len(inputs) > 0 {
		payload["inputs"] = inputs
	}
	var authorization openCodeOAuthAuthorization
	if err := d.doOpenCodeJSON(ctx, http.MethodPost, "/provider/"+url.PathEscape(provider.ID)+"/oauth/authorize", payload, &authorization); err != nil {
		d.releaseProviderOperation(provider.ID, attemptID)
		slog.Error("agent providers: OAuth authorize failed", "provider", provider.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 未能启动授权"})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(authorization.Method))
	if mode != "auto" && mode != "code" {
		d.releaseProviderOperation(provider.ID, attemptID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 返回了不支持的授权方式"})
		return
	}
	if !validAuthorizationURL(authorization.URL) {
		d.releaseProviderOperation(provider.ID, attemptID)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 返回了无效的授权地址"})
		return
	}
	oauthCtx, oauthCancel := context.WithTimeout(context.Background(), providerOAuthLifetime)
	now := time.Now()
	attempt := &agentProviderOAuthAttempt{
		ID: attemptID, ProviderID: provider.ID, ProviderName: provider.Name,
		MethodIndex: method.Index, MethodLabel: method.Label, Mode: mode,
		URL: authorization.URL, Instructions: strings.TrimSpace(authorization.Instructions),
		Code: providerVerificationCode(authorization.Instructions), Status: "pending",
		CreatedAt: now, ExpiresAt: now.Add(providerOAuthLifetime), Context: oauthCtx, Cancel: oauthCancel,
	}
	d.providerMu.Lock()
	d.cleanupProviderAttemptsLocked(now)
	d.providerAttempts[attempt.ID] = attempt
	d.providerMu.Unlock()
	response := providerAttemptResponse(attempt)
	if mode == "auto" {
		go d.completeProviderOAuthAttempt(attempt.ID, "")
	}
	writeJSON(w, http.StatusCreated, response)
}

func (d *AgentDeps) GetProviderOAuthAttempt(w http.ResponseWriter, r *http.Request) {
	attempt, ok := d.providerAttempt(r.PathValue("attemptID"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "授权已结束或不存在"})
		return
	}
	writeJSON(w, http.StatusOK, providerAttemptResponse(attempt))
}

func (d *AgentDeps) CompleteProviderOAuth(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "授权码格式无效"})
		return
	}
	attemptID := strings.TrimSpace(r.PathValue("attemptID"))
	d.providerMu.Lock()
	d.cleanupProviderAttemptsLocked(time.Now())
	attempt, ok := d.providerAttempts[attemptID]
	if !ok {
		d.providerMu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "授权已结束或不存在"})
		return
	}
	if attempt.Mode != "code" || attempt.Status != "pending" || attempt.Completing {
		d.providerMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该授权当前不能提交授权码"})
		return
	}
	code := strings.TrimSpace(input.Code)
	if code == "" || len(code) > 2048 || !utf8.ValidString(code) {
		d.providerMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请输入有效的授权码"})
		return
	}
	attempt.Completing = true
	d.providerMu.Unlock()

	d.completeProviderOAuthAttempt(attemptID, code)
	result, ok := d.providerAttempt(attemptID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "授权已结束或不存在"})
		return
	}
	status := http.StatusOK
	if result.Status == "failed" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, providerAttemptResponse(result))
}

func (d *AgentDeps) CancelProviderOAuth(w http.ResponseWriter, r *http.Request) {
	attemptID := strings.TrimSpace(r.PathValue("attemptID"))
	d.providerMu.Lock()
	d.cleanupProviderAttemptsLocked(time.Now())
	attempt, ok := d.providerAttempts[attemptID]
	if !ok {
		d.providerMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if attempt.Status == "pending" {
		attempt.Status = "cancelled"
		attempt.CompletedAt = time.Now()
		if attempt.Cancel != nil {
			attempt.Cancel()
		}
	}
	d.releaseProviderOperationLocked(attempt.ProviderID, attempt.ID)
	d.providerMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (d *AgentDeps) DisconnectProvider(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.PathValue("providerID"))
	ctx, cancel := context.WithTimeout(r.Context(), agentSettingsTimeout)
	defer cancel()
	providers, err := d.providerCatalog(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "无法读取 OpenCode 提供商列表"})
		return
	}
	provider := findAgentProvider(providers, providerID)
	if provider == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "提供商不存在"})
		return
	}
	if !provider.Connected {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !provider.CanDisconnect {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该连接来自环境配置，不能在这里断开", "code": "agent_provider_managed"})
		return
	}
	if d.settingsStore != nil {
		selected := d.settingsStore.Get().Model
		if selected != nil && selected.ProviderID == provider.ID {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "当前模型正在使用该提供商，请先在高阶设置中切换模型", "code": "agent_provider_in_use",
			})
			return
		}
	}
	opID, err := d.reserveProviderOperation(provider.ID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该提供商已有连接操作正在进行", "code": "agent_provider_busy"})
		return
	}
	defer d.releaseProviderOperation(provider.ID, opID)
	for _, credentialID := range provider.CredentialIDs {
		path := openCodeWorkspacePath("/api/credential/" + url.PathEscape(credentialID))
		if err := d.doOpenCodeJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
			slog.Error("agent providers: disconnect failed", "provider", provider.ID, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 未能断开该连接"})
			return
		}
	}
	if provider.LegacyAuth {
		if err := d.doOpenCodeJSON(ctx, http.MethodDelete, "/auth/"+url.PathEscape(provider.ID), nil, nil); err != nil {
			slog.Error("agent providers: legacy disconnect failed", "provider", provider.ID, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "OpenCode 未能断开该连接"})
			return
		}
		if err := d.disposeOpenCode(ctx); err != nil {
			slog.Error("agent providers: refresh after disconnect failed", "provider", provider.ID, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "连接已断开，但 OpenCode 刷新失败，请重试"})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *AgentDeps) providerCatalog(ctx context.Context) ([]agentProviderOption, error) {
	var providersResponse openCodeProviderResponse
	if err := d.getOpenCodeJSON(ctx, "/provider", &providersResponse); err != nil {
		return nil, err
	}
	methodsByProvider := make(map[string][]openCodeProviderAuthMethod)
	if err := d.getOpenCodeJSON(ctx, "/provider/auth", &methodsByProvider); err != nil {
		return nil, err
	}
	var integrationsResponse openCodeIntegrationListResponse
	integrationsPath := openCodeWorkspacePath("/api/integration")
	if err := d.getOpenCodeJSON(ctx, integrationsPath, &integrationsResponse); err != nil {
		return nil, err
	}
	type connectionState struct {
		connected     bool
		canDisconnect bool
		credentialIDs []string
	}
	connections := make(map[string]connectionState, len(integrationsResponse.Data))
	for _, integration := range integrationsResponse.Data {
		id := strings.TrimSpace(integration.ID)
		if id == "" {
			continue
		}
		state := connectionState{}
		for _, connection := range integration.Connections {
			switch strings.ToLower(strings.TrimSpace(connection.Type)) {
			case "credential":
				credentialID := strings.TrimSpace(connection.ID)
				if credentialID != "" {
					state.connected = true
					state.canDisconnect = true
					state.credentialIDs = append(state.credentialIDs, credentialID)
				}
			case "env":
				state.connected = true
			}
		}
		connections[id] = state
	}
	legacyConnected := make(map[string]struct{}, len(providersResponse.Connected))
	for _, id := range providersResponse.Connected {
		legacyConnected[strings.TrimSpace(id)] = struct{}{}
	}
	providers := make([]agentProviderOption, 0, len(providersResponse.All))
	for _, upstream := range providersResponse.All {
		id := strings.TrimSpace(upstream.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(upstream.Name)
		if name == "" {
			name = id
		}
		connection := connections[id]
		source := strings.ToLower(strings.TrimSpace(upstream.Source))
		legacyAuth := false
		if _, listed := legacyConnected[id]; listed {
			switch source {
			case "api":
				legacyAuth = true
			case "env", "config":
				connection.connected = true
			default:
				legacyAuth = len(methodsByProvider[id]) > 0
			}
		}
		if legacyAuth {
			connection.connected = true
			connection.canDisconnect = true
		}
		methods := sanitizedProviderMethods(id, methodsByProvider[id])
		providers = append(providers, agentProviderOption{
			ID: id, Name: name, Source: strings.TrimSpace(upstream.Source), Connected: connection.connected,
			CanDisconnect: connection.canDisconnect, ModelCount: len(upstream.Models), Methods: methods,
			CredentialIDs: connection.credentialIDs, LegacyAuth: legacyAuth,
		})
	}
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].Connected != providers[j].Connected {
			return providers[i].Connected
		}
		leftName := strings.ToLower(providers[i].Name)
		rightName := strings.ToLower(providers[j].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return providers[i].ID < providers[j].ID
	})
	return providers, nil
}

func sanitizedProviderMethods(providerID string, methods []openCodeProviderAuthMethod) []agentProviderMethod {
	result := make([]agentProviderMethod, 0, len(methods)+1)
	for index, method := range methods {
		methodType := strings.ToLower(strings.TrimSpace(method.Type))
		if methodType == "key" {
			methodType = "api"
		}
		if methodType != "api" && methodType != "oauth" {
			continue
		}
		label := strings.TrimSpace(method.Label)
		if label == "" {
			if methodType == "oauth" {
				label = "OAuth 授权"
			} else {
				label = "API Key"
			}
		}
		if providerID == "openai" && methodType == "oauth" && strings.Contains(strings.ToLower(label), "browser") {
			continue
		}
		result = append(result, agentProviderMethod{
			Index: index, Type: methodType, Label: label,
			Prompts: sanitizeProviderPrompts(method.Prompts), Available: true,
		})
	}
	if len(result) == 0 {
		result = append(result, agentProviderMethod{Index: -1, Type: "api", Label: "API Key", Available: true})
	}
	return result
}

func sanitizeProviderPrompts(prompts []openCodeProviderPrompt) []openCodeProviderPrompt {
	result := make([]openCodeProviderPrompt, 0, len(prompts))
	for _, prompt := range prompts {
		prompt.Type = strings.ToLower(strings.TrimSpace(prompt.Type))
		prompt.Key = strings.TrimSpace(prompt.Key)
		if (prompt.Type != "text" && prompt.Type != "select") || prompt.Key == "" || len(prompt.Key) > 128 {
			continue
		}
		prompt.Message = strings.TrimSpace(prompt.Message)
		prompt.Placeholder = strings.TrimSpace(prompt.Placeholder)
		if prompt.When != nil {
			prompt.When.Key = strings.TrimSpace(prompt.When.Key)
			prompt.When.Op = strings.TrimSpace(prompt.When.Op)
			prompt.When.Value = strings.TrimSpace(prompt.When.Value)
		}
		if prompt.Type == "select" {
			options := make([]openCodeProviderOption, 0, len(prompt.Options))
			for _, option := range prompt.Options {
				option.Label = strings.TrimSpace(option.Label)
				option.Value = strings.TrimSpace(option.Value)
				option.Hint = strings.TrimSpace(option.Hint)
				if option.Label != "" && option.Value != "" && len(option.Value) <= maxAgentProviderInput {
					options = append(options, option)
				}
			}
			prompt.Options = options
		}
		result = append(result, prompt)
	}
	return result
}

func (d *AgentDeps) resolveProviderMethod(ctx context.Context, providerID string, methodIndex int, methodType string) (*agentProviderOption, *agentProviderMethod, error) {
	providers, err := d.providerCatalog(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("catalog: %w", err)
	}
	provider := findAgentProvider(providers, providerID)
	if provider == nil {
		return nil, nil, errProviderNotFound
	}
	for index := range provider.Methods {
		method := &provider.Methods[index]
		if method.Index == methodIndex && method.Type == methodType {
			return provider, method, nil
		}
	}
	return nil, nil, errProviderMethodInvalid
}

var (
	errProviderNotFound      = errors.New("provider not found")
	errProviderMethodInvalid = errors.New("provider method invalid")
	errProviderBusy          = errors.New("provider operation busy")
)

func writeProviderValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errProviderNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "提供商不存在"})
	case errors.Is(err, errProviderMethodInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "所选连接方式不可用"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "无法读取 OpenCode 提供商列表"})
	}
}

func findAgentProvider(providers []agentProviderOption, id string) *agentProviderOption {
	for index := range providers {
		if providers[index].ID == id {
			return &providers[index]
		}
	}
	return nil
}

func validatedProviderInputs(prompts []openCodeProviderPrompt, input map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	allowed := make(map[string]openCodeProviderPrompt, len(prompts))
	for _, prompt := range prompts {
		allowed[prompt.Key] = prompt
	}
	for key := range input {
		if _, ok := allowed[key]; !ok {
			return nil, errors.New("连接参数无效")
		}
	}
	for _, prompt := range prompts {
		if !providerPromptVisible(prompt, input) {
			continue
		}
		value := strings.TrimSpace(input[prompt.Key])
		if len(value) > maxAgentProviderInput || !utf8.ValidString(value) {
			return nil, fmt.Errorf("%s 内容无效", providerPromptLabel(prompt))
		}
		if prompt.Type == "select" {
			valid := false
			for _, option := range prompt.Options {
				if option.Value == value {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("请选择有效的%s", providerPromptLabel(prompt))
			}
		} else if value == "" && !strings.Contains(strings.ToLower(prompt.Message), "optional") {
			return nil, fmt.Errorf("请输入%s", providerPromptLabel(prompt))
		}
		if value != "" {
			result[prompt.Key] = value
		}
	}
	return result, nil
}

func providerPromptVisible(prompt openCodeProviderPrompt, input map[string]string) bool {
	if prompt.When == nil {
		return true
	}
	actual := input[prompt.When.Key]
	switch strings.ToLower(prompt.When.Op) {
	case "eq", "equals", "==":
		return actual == prompt.When.Value
	case "neq", "not_equals", "!=":
		return actual != prompt.When.Value
	default:
		return true
	}
}

func providerPromptLabel(prompt openCodeProviderPrompt) string {
	if prompt.Message != "" {
		return prompt.Message
	}
	return prompt.Key
}

func (d *AgentDeps) reserveProviderOperation(providerID string) (string, error) {
	id, err := randomProviderAttemptID()
	if err != nil {
		return "", err
	}
	return id, d.reserveProviderOperationWithID(providerID, id)
}

func (d *AgentDeps) reserveProviderOperationWithID(providerID, operationID string) error {
	d.providerMu.Lock()
	defer d.providerMu.Unlock()
	d.cleanupProviderAttemptsLocked(time.Now())
	if _, busy := d.providerBusy[providerID]; busy {
		return errProviderBusy
	}
	d.providerBusy[providerID] = operationID
	return nil
}

func (d *AgentDeps) releaseProviderOperation(providerID, operationID string) {
	d.providerMu.Lock()
	defer d.providerMu.Unlock()
	d.releaseProviderOperationLocked(providerID, operationID)
}

func (d *AgentDeps) releaseProviderOperationLocked(providerID, operationID string) {
	if d.providerBusy[providerID] == operationID {
		delete(d.providerBusy, providerID)
	}
}

func (d *AgentDeps) providerAttempt(id string) (*agentProviderOAuthAttempt, bool) {
	d.providerMu.Lock()
	defer d.providerMu.Unlock()
	d.cleanupProviderAttemptsLocked(time.Now())
	attempt, ok := d.providerAttempts[strings.TrimSpace(id)]
	if !ok {
		return nil, false
	}
	copy := *attempt
	copy.Context = nil
	copy.Cancel = nil
	return &copy, true
}

func (d *AgentDeps) cleanupProviderAttemptsLocked(now time.Time) {
	for id, attempt := range d.providerAttempts {
		if attempt.Status == "pending" && !now.Before(attempt.ExpiresAt) {
			attempt.Status = "expired"
			attempt.CompletedAt = now
			if attempt.Cancel != nil {
				attempt.Cancel()
			}
			d.releaseProviderOperationLocked(attempt.ProviderID, attempt.ID)
		}
		if attempt.Status != "pending" && !attempt.CompletedAt.IsZero() && now.Sub(attempt.CompletedAt) > providerAttemptKeep {
			delete(d.providerAttempts, id)
		}
	}
}

func (d *AgentDeps) completeProviderOAuthAttempt(attemptID, code string) {
	d.providerMu.Lock()
	attempt, ok := d.providerAttempts[attemptID]
	if !ok || attempt.Status != "pending" {
		d.providerMu.Unlock()
		return
	}
	providerID := attempt.ProviderID
	methodIndex := attempt.MethodIndex
	ctx := attempt.Context
	d.providerMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	payload := map[string]any{"method": methodIndex}
	if code != "" {
		payload["code"] = code
	}
	var connected bool
	err := d.doOpenCodeJSON(ctx, http.MethodPost, "/provider/"+url.PathEscape(providerID)+"/oauth/callback", payload, &connected)
	if err == nil && !connected {
		err = errors.New("OpenCode rejected OAuth callback")
	}
	if err == nil {
		err = d.disposeOpenCode(ctx)
	}

	d.providerMu.Lock()
	defer d.providerMu.Unlock()
	attempt, ok = d.providerAttempts[attemptID]
	if !ok || attempt.Status != "pending" {
		return
	}
	attempt.Completing = false
	attempt.CompletedAt = time.Now()
	if attempt.Cancel != nil {
		attempt.Cancel()
	}
	if err == nil {
		attempt.Status = "succeeded"
	} else if errors.Is(err, context.DeadlineExceeded) {
		attempt.Status = "expired"
		attempt.Error = "授权已超时，请重新连接"
	} else if errors.Is(err, context.Canceled) {
		attempt.Status = "cancelled"
	} else {
		attempt.Status = "failed"
		attempt.Error = "OpenCode 未能完成授权，请重试"
		slog.Error("agent providers: OAuth callback failed", "provider", providerID, "error", err)
	}
	d.releaseProviderOperationLocked(providerID, attemptID)
}

func (d *AgentDeps) disposeOpenCode(ctx context.Context) error {
	return d.doOpenCodeJSON(ctx, http.MethodPost, "/instance/dispose?directory="+url.QueryEscape("/workspace"), nil, nil)
}

func openCodeWorkspacePath(path string) string {
	return path + "?location%5Bdirectory%5D=" + url.QueryEscape("/workspace")
}

func (d *AgentDeps) doOpenCodeJSON(ctx context.Context, method, path string, payload, target any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.openCodeURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OpenCode %s %s returned %d", method, path, resp.StatusCode)
	}
	if target == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxAgentProviderResponse+1))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func randomProviderAttemptID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func validAuthorizationURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func providerVerificationCode(instructions string) string {
	match := providerCodePattern.FindStringSubmatch(instructions)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func providerAttemptResponse(attempt *agentProviderOAuthAttempt) agentProviderOAuthAttemptResponse {
	return agentProviderOAuthAttemptResponse{
		ID: attempt.ID, ProviderID: attempt.ProviderID, ProviderName: attempt.ProviderName,
		MethodLabel: attempt.MethodLabel, Mode: attempt.Mode, URL: attempt.URL,
		Instructions: attempt.Instructions, Code: attempt.Code, Status: attempt.Status,
		Error: attempt.Error, CreatedAt: attempt.CreatedAt.UnixMilli(), ExpiresAt: attempt.ExpiresAt.UnixMilli(),
	}
}
