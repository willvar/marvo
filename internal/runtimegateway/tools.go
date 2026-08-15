package runtimegateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"marvo/internal/runtimeevents"
	"marvo/internal/store"
	"net/http"
	"path/filepath"
	"strings"
)

const maxAgentToolBody = 256 << 10

func (s *Server) authenticateTool(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("userID")
		if !validUserID(userID) {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid user"})
			return
		}
		provided := strings.TrimSpace(r.Header.Get("X-Marvo-Tool-Token"))
		expected := agentToolToken(s.token, userID)
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeGatewayJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleTool(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentToolBody)
	if r.PathValue("tool") == "sessions" {
		s.handleSessionsTool(w, r)
		return
	}
	workspace := filepath.Join(runtimeStateRoot(s.runtimes), "users", userID, "workspace")
	state, err := store.OpenStateDB(workspace)
	if err != nil {
		writeGatewayJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Marvo user state is unavailable"})
		return
	}
	defer state.Close()
	switch r.PathValue("tool") {
	case "activity":
		s.handleActivityTool(state, w, r)
	case "memories":
		s.handleMemoriesTool(state, w, r)
	case "space":
		s.handleSpaceTool(state, w, r)
	case "agent-settings":
		s.handleAgentSettingsTool(state, w, r)
	case "devices":
		s.handleDevicesTool(state, w, r)
	default:
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "unknown Marvo tool"})
	}
}

func (s *Server) handleActivityTool(state *store.StateDB, w http.ResponseWriter, r *http.Request) {
	var input store.ActivityPublish
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Activity"})
		return
	}
	activities, err := store.NewActivityStore(state)
	if err != nil {
		writeGatewayJSON(w, http.StatusInternalServerError, map[string]any{"error": "Activity store is unavailable"})
		return
	}
	_, created, err := activities.Publish(input)
	if err != nil {
		writeToolError(w, err)
		return
	}
	if created {
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindActivity)
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{"published": true})
}

func (s *Server) handleMemoriesTool(state *store.StateDB, w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Text   string `json:"text"`
	}
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid memories operation"})
		return
	}
	memories, err := store.NewMemoryStore(state)
	if err != nil {
		writeGatewayJSON(w, http.StatusInternalServerError, map[string]any{"error": "memory store is unavailable"})
		return
	}
	switch input.Action {
	case "list":
		if input.ID != "" || input.Text != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "list does not accept id or text"})
			return
		}
		snapshot, err := memories.Get()
		if err != nil {
			writeToolError(w, err)
			return
		}
		writeGatewayJSON(w, http.StatusOK, snapshot)
	case "add":
		if input.ID != "" || strings.TrimSpace(input.Text) == "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "add requires only text"})
			return
		}
		memory, err := memories.Add(input.Text)
		if err != nil {
			writeToolError(w, err)
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindMemories)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"memory": memory})
	case "update":
		if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Text) == "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "update requires id and text"})
			return
		}
		memory, err := memories.Update(input.ID, input.Text)
		if err != nil {
			writeToolError(w, err)
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindMemories)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"memory": memory})
	case "remove":
		if strings.TrimSpace(input.ID) == "" || input.Text != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "remove requires only id"})
			return
		}
		removed, err := memories.Remove(input.ID)
		if err != nil {
			writeToolError(w, err)
			return
		}
		if !removed {
			writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "memory not found"})
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindMemories)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"removed": true})
	default:
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported memories operation"})
	}
}

func (s *Server) handleSpaceTool(state *store.StateDB, w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid space operation"})
		return
	}
	brand, err := store.NewBrandStore(state)
	if err != nil {
		writeToolError(w, err)
		return
	}
	switch input.Action {
	case "get":
		if input.Name != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "get does not accept name"})
			return
		}
		writeGatewayJSON(w, http.StatusOK, map[string]any{"brand": brand.Get()})
	case "set_brand":
		if strings.TrimSpace(input.Name) == "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "set_brand requires name"})
			return
		}
		updated, err := brand.Save(input.Name)
		if err != nil {
			writeToolError(w, err)
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindSpace)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"brand": updated})
	default:
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported space operation"})
	}
}

func (s *Server) handleAgentSettingsTool(state *store.StateDB, w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action       string  `json:"action"`
		ProviderID   *string `json:"provider_id"`
		ModelID      *string `json:"model_id"`
		Variant      *string `json:"variant"`
		GlobalPrompt *string `json:"global_prompt"`
		ClearModel   bool    `json:"clear_model"`
	}
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Agent settings operation"})
		return
	}
	settingsStore, err := store.NewAgentSettingsStore(state)
	if err != nil {
		writeToolError(w, err)
		return
	}
	settings := settingsStore.Get()
	if input.Action == "get" {
		if input.ProviderID != nil || input.ModelID != nil || input.Variant != nil || input.GlobalPrompt != nil || input.ClearModel {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "get does not accept setting values"})
			return
		}
		writeGatewayJSON(w, http.StatusOK, map[string]any{"settings": settings})
		return
	}
	if input.Action != "update" {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported Agent settings operation"})
		return
	}
	if (input.ProviderID == nil) != (input.ModelID == nil) {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "provider_id and model_id must be updated together"})
		return
	}
	if input.ClearModel && input.ProviderID != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "clear_model conflicts with provider_id and model_id"})
		return
	}
	if input.ProviderID == nil && input.Variant == nil && input.GlobalPrompt == nil && !input.ClearModel {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "update requires at least one setting"})
		return
	}
	if input.ClearModel {
		settings.Model = nil
		settings.Variant = ""
	}
	if input.ProviderID != nil && input.ModelID != nil {
		settings.Model = &store.AgentModelSelection{ProviderID: *input.ProviderID, ModelID: *input.ModelID}
	}
	if input.Variant != nil {
		settings.Variant = *input.Variant
	}
	if input.GlobalPrompt != nil {
		settings.GlobalPrompt = *input.GlobalPrompt
	}
	if err := settingsStore.Save(settings); err != nil {
		writeToolError(w, err)
		return
	}
	s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindAgentSettings)
	writeGatewayJSON(w, http.StatusOK, map[string]any{"settings": settingsStore.Get()})
}

func (s *Server) handleDevicesTool(state *store.StateDB, w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action        string `json:"action"`
		ID            string `json:"id"`
		LocalDeviceID string `json:"local_device_id"`
		Name          string `json:"name"`
	}
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid devices operation"})
		return
	}
	devices := store.NewDeviceStore(state, "agent-tool-does-not-sign-device-cookies")
	switch input.Action {
	case "list":
		if input.ID != "" || input.LocalDeviceID != "" || input.Name != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "list does not accept device values"})
			return
		}
		writeGatewayJSON(w, http.StatusOK, map[string]any{"requests": devices.ListRequests(), "devices": devices.ListDevices()})
	case "approve":
		if strings.TrimSpace(input.ID) == "" || input.LocalDeviceID != "" || input.Name != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "approve requires only id"})
			return
		}
		device, err := devices.ApproveRequest(input.ID)
		if err != nil {
			writeToolError(w, err)
			return
		}
		if device == nil {
			writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device request not found"})
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindDevices)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"device": device})
	case "reject":
		if strings.TrimSpace(input.ID) == "" || input.LocalDeviceID != "" || input.Name != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "reject requires only id"})
			return
		}
		removed, err := devices.RejectRequest(input.ID)
		if err != nil {
			writeToolError(w, err)
			return
		}
		if !removed {
			writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device request not found"})
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindDevices)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"removed": true})
	case "rename":
		if input.ID != "" || strings.TrimSpace(input.LocalDeviceID) == "" || strings.TrimSpace(input.Name) == "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "rename requires local_device_id and name"})
			return
		}
		device, err := devices.RenameDevice(input.LocalDeviceID, input.Name)
		if err != nil {
			writeToolError(w, err)
			return
		}
		if device == nil {
			writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device not found"})
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindDevices)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"device": device})
	case "revoke":
		if input.ID != "" || strings.TrimSpace(input.LocalDeviceID) == "" || input.Name != "" {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "revoke requires only local_device_id"})
			return
		}
		removed, err := devices.RevokeDevice(input.LocalDeviceID)
		if err != nil {
			writeToolError(w, err)
			return
		}
		if !removed {
			writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "device not found"})
			return
		}
		s.publishStateEvent(r.PathValue("userID"), runtimeevents.KindDevices)
		writeGatewayJSON(w, http.StatusOK, map[string]any{"removed": true})
	default:
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported devices operation"})
	}
}

func decodeToolJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentToolBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func writeToolError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrInvalidActivity), errors.Is(err, store.ErrInvalidMemories),
		errors.Is(err, store.ErrInvalidAgentSettings), errors.Is(err, store.ErrInvalidBrand),
		errors.Is(err, store.ErrInvalidDeviceName):
		status = http.StatusBadRequest
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrActivityNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrDeviceNameConflict), errors.Is(err, store.ErrActivityResponded):
		status = http.StatusConflict
	}
	writeGatewayJSON(w, status, map[string]any{"error": fmt.Sprintf("%v", err)})
}

type stateRootProvider interface {
	StateRoot() string
}

func runtimeStateRoot(provider RuntimeProvider) string {
	if value, ok := provider.(stateRootProvider); ok {
		return value.StateRoot()
	}
	return "/state"
}

func (m *RuntimeManager) StateRoot() string {
	return m.config.StateDir
}

func agentToolToken(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("marvo-agent-tool-v1\x00" + userID))
	return hex.EncodeToString(mac.Sum(nil))
}
