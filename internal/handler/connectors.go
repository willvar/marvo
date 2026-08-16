package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"marvo/internal/connectors"
	"marvo/internal/store"
)

const maxConnectorRequestBody = store.MaxConnectorConfigSize + 16<<10

type connectorResponse struct {
	ID               string                         `json:"id"`
	ProviderID       string                         `json:"provider_id"`
	ProviderName     string                         `json:"provider_name"`
	Name             string                         `json:"name"`
	Enabled          bool                           `json:"enabled"`
	Config           map[string]any                 `json:"config"`
	SecretConfigured map[string]bool                `json:"secret_configured"`
	Delivery         store.ConnectorDeliverySummary `json:"delivery"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
}

type connectorCreateRequest struct {
	ProviderID string         `json:"provider_id"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Config     map[string]any `json:"config"`
}

type connectorUpdateRequest struct {
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	Config       map[string]any `json:"config"`
	ClearSecrets []string       `json:"clear_secrets"`
}

type connectorTestRequest struct {
	ConnectorID  string         `json:"connector_id"`
	ProviderID   string         `json:"provider_id"`
	Config       map[string]any `json:"config"`
	ClearSecrets []string       `json:"clear_secrets"`
}

func (d *Dependencies) ListConnectorProviders(w http.ResponseWriter, _ *http.Request) {
	if d.Providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "连接器服务不可用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": d.Providers.Catalog()})
}

func (d *Dependencies) ListConnectors(w http.ResponseWriter, _ *http.Request) {
	if d.Connectors == nil || d.Providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "连接器服务不可用"})
		return
	}
	items, err := d.Connectors.List()
	if err != nil {
		slog.Error("connectors: list failed", "user_id", d.UserID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取连接器失败"})
		return
	}
	result := make([]connectorResponse, 0, len(items))
	for _, item := range items {
		response, err := d.connectorResponse(item)
		if err != nil {
			slog.Error("connectors: build response failed", "user_id", d.UserID, "connector_id", item.ID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取连接器状态失败"})
			return
		}
		result = append(result, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectors": result})
}

func (d *Dependencies) CreateConnector(w http.ResponseWriter, r *http.Request) {
	if d.Connectors == nil || d.Providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "连接器服务不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConnectorRequestBody)
	var body connectorCreateRequest
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器设置格式无效"})
		return
	}
	normalized, err := d.Providers.Validate(strings.TrimSpace(body.ProviderID), body.Config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := d.Connectors.Create(body.ProviderID, body.Name, body.Enabled, normalized)
	if errors.Is(err, store.ErrInvalidConnector) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器名称或设置无效"})
		return
	}
	if err != nil {
		slog.Error("connectors: create failed", "user_id", d.UserID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "创建连接器失败"})
		return
	}
	response, err := d.connectorResponse(item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "连接器已创建，但读取状态失败"})
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (d *Dependencies) UpdateConnector(w http.ResponseWriter, r *http.Request) {
	if d.Connectors == nil || d.Providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "连接器服务不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConnectorRequestBody)
	var body connectorUpdateRequest
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器设置格式无效"})
		return
	}
	existing, err := d.Connectors.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrConnectorNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "连接器不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取连接器失败"})
		return
	}
	config, err := d.mergeConnectorConfig(existing, body.Config, body.ClearSecrets)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	item, err := d.Connectors.Update(existing.ID, body.Name, body.Enabled, config)
	if errors.Is(err, store.ErrInvalidConnector) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器名称或设置无效"})
		return
	}
	if err != nil {
		slog.Error("connectors: update failed", "user_id", d.UserID, "connector_id", existing.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存连接器失败"})
		return
	}
	response, err := d.connectorResponse(item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "连接器已保存，但读取状态失败"})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (d *Dependencies) DeleteConnector(w http.ResponseWriter, r *http.Request) {
	deleted, err := d.Connectors.Delete(r.PathValue("id"))
	if errors.Is(err, store.ErrInvalidConnector) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器 ID 无效"})
		return
	}
	if err != nil {
		slog.Error("connectors: delete failed", "user_id", d.UserID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "删除连接器失败"})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "连接器不存在"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Dependencies) TestConnector(w http.ResponseWriter, r *http.Request) {
	if d.Connectors == nil || d.Providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "连接器服务不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConnectorRequestBody)
	var body connectorTestRequest
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "连接器设置格式无效"})
		return
	}
	providerID := strings.TrimSpace(body.ProviderID)
	config := body.Config
	if body.ConnectorID != "" {
		existing, err := d.Connectors.Get(body.ConnectorID)
		if errors.Is(err, store.ErrConnectorNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "连接器不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取连接器失败"})
			return
		}
		providerID = existing.ProviderID
		config, err = d.mergeConnectorConfig(existing, body.Config, body.ClearSecrets)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	} else {
		var err error
		config, err = d.Providers.Validate(providerID, config)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}

	testID := randomConnectorTestID()
	testURL := d.Config.Server.PublicURL + "/user/" + d.UserID + "/activity"
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	err := d.Providers.Send(ctx, providerID, config, connectors.Message{
		DeliveryID: testID, ActivityID: testID, Kind: store.ActivityKindNotice,
		Title: "Marvo 连接测试", Content: "如果你收到这条消息，说明 Activity 连接器可以正常发送。",
		URL: testURL, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		status := http.StatusBadGateway
		if connectors.IsPermanent(err) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": d.Providers.RedactError(providerID, config, err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Dependencies) RetryConnectorDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, err := d.Connectors.Get(r.PathValue("id")); errors.Is(err, store.ErrConnectorNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "连接器不存在"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取连接器失败"})
		return
	}
	retried, err := d.Connectors.RetryConnectorFailures(r.PathValue("id"), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "重新投递失败"})
		return
	}
	if retried > 0 && d.Spaces != nil {
		d.Spaces.WakeDeliveries(d.UserID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"retried": retried})
}

func (d *Dependencies) connectorResponse(item store.Connector) (connectorResponse, error) {
	provider, ok := d.Providers.Provider(item.ProviderID)
	if !ok {
		return connectorResponse{}, errors.New("connector provider is unavailable")
	}
	publicConfig := make(map[string]any)
	configured := make(map[string]bool)
	for _, field := range provider.Fields {
		value, exists := item.Config[field.Key]
		if !exists {
			continue
		}
		if field.Type == connectors.FieldSecret || field.Sensitive {
			configured[field.Key] = !emptyConnectorValue(value)
			continue
		}
		publicConfig[field.Key] = value
	}
	summary, err := d.Connectors.Summary(item.ID)
	if err != nil {
		return connectorResponse{}, err
	}
	return connectorResponse{
		ID: item.ID, ProviderID: item.ProviderID, ProviderName: provider.Name,
		Name: item.Name, Enabled: item.Enabled, Config: publicConfig, SecretConfigured: configured,
		Delivery: summary, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}, nil
}

func (d *Dependencies) mergeConnectorConfig(existing store.Connector, submitted map[string]any, clearSecrets []string) (map[string]any, error) {
	provider, ok := d.Providers.Provider(existing.ProviderID)
	if !ok {
		return nil, errors.New("连接器类型已不可用")
	}
	if submitted == nil {
		submitted = map[string]any{}
	}
	allowed := make(map[string]connectors.Field, len(provider.Fields))
	for _, field := range provider.Fields {
		allowed[field.Key] = field
	}
	for key := range submitted {
		if _, exists := allowed[key]; !exists {
			return nil, errors.New("连接器设置中包含未知字段")
		}
	}
	clear := make(map[string]bool, len(clearSecrets))
	for _, key := range clearSecrets {
		clear[strings.TrimSpace(key)] = true
	}
	merged := make(map[string]any)
	for _, field := range provider.Fields {
		if field.Type != connectors.FieldSecret && !field.Sensitive {
			if value, exists := submitted[field.Key]; exists {
				merged[field.Key] = value
			} else if value, exists := existing.Config[field.Key]; exists {
				merged[field.Key] = value
			}
			continue
		}
		if clear[field.Key] {
			continue
		}
		if value, exists := submitted[field.Key]; exists && !emptyConnectorValue(value) {
			merged[field.Key] = value
		} else if value, exists := existing.Config[field.Key]; exists {
			merged[field.Key] = value
		}
	}
	for key := range clear {
		valid := false
		field, exists := allowed[key]
		valid = exists && (field.Type == connectors.FieldSecret || field.Sensitive)
		if !valid {
			return nil, errors.New("要清除的凭据字段无效")
		}
	}
	return d.Providers.Validate(existing.ProviderID, merged)
}

func emptyConnectorValue(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

func randomConnectorTestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return strings.Repeat("0", 32)
	}
	return hex.EncodeToString(value[:])
}
