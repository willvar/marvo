package handler

import (
	"context"
	"errors"
	"log/slog"
	"marvo/internal/store"
	"net/http"
)

type personalizationResponse struct {
	Rules         []store.PersonalizationRule `json:"rules"`
	Revision      string                      `json:"revision"`
	PromptPending bool                        `json:"prompt_pending"`
}

type personalizationUpdate struct {
	Revision string                      `json:"revision"`
	Rules    []store.PersonalizationRule `json:"rules"`
}

func (d *AgentDeps) GetPersonalization(w http.ResponseWriter, r *http.Request) {
	if d.personalization == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "个性化规则不可用"})
		return
	}
	snapshot, err := d.personalization.Get()
	if err != nil {
		slog.Error("agent personalization: load failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取个性化规则失败"})
		return
	}
	pending, err := d.activatePersonalizationPrompt(r.Context())
	if err != nil {
		slog.Error("agent personalization: activate prompt failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法应用个性化规则"})
		return
	}
	writeJSON(w, http.StatusOK, personalizationResponse{Rules: snapshot.Rules, Revision: snapshot.Revision, PromptPending: pending})
}

func (d *AgentDeps) UpdatePersonalization(w http.ResponseWriter, r *http.Request) {
	if d.personalization == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "个性化规则不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxPersonalizationBytes+4096)
	var update personalizationUpdate
	if err := readJSON(r, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "个性化规则格式无效"})
		return
	}
	snapshot, err := d.personalization.Save(update.Revision, update.Rules)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPersonalizationChanged):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    "个性化规则已经发生变化，请重新加载后再编辑",
				"rules":    snapshot.Rules,
				"revision": snapshot.Revision,
			})
		case errors.Is(err, store.ErrInvalidPersonalization):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "个性化规则内容无效"})
		default:
			slog.Error("agent personalization: save failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存个性化规则失败"})
		}
		return
	}
	pending, activateErr := d.activatePersonalizationPrompt(r.Context())
	if activateErr != nil {
		slog.Error("agent personalization: activate saved prompt failed", "error", activateErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "规则已保存，但暂时无法应用"})
		return
	}
	writeJSON(w, http.StatusOK, personalizationResponse{Rules: snapshot.Rules, Revision: snapshot.Revision, PromptPending: pending})
}

func (d *AgentDeps) activatePersonalizationPrompt(ctx context.Context) (bool, error) {
	if d.settingsStore == nil || d.globalPromptFile == nil {
		return false, nil
	}
	return d.activateSavedGlobalPrompt(ctx)
}
