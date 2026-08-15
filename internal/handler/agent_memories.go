package handler

import (
	"context"
	"errors"
	"log/slog"
	"marvo/internal/store"
	"net/http"
)

type memoriesResponse struct {
	Memories      []store.Memory `json:"memories"`
	Revision      string         `json:"revision"`
	PromptPending bool           `json:"prompt_pending"`
}

type memoriesUpdate struct {
	Revision string         `json:"revision"`
	Memories []store.Memory `json:"memories"`
}

func (d *AgentDeps) GetMemories(w http.ResponseWriter, r *http.Request) {
	if d.memories == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "记忆不可用"})
		return
	}
	snapshot, err := d.memories.Get()
	if err != nil {
		slog.Error("agent memories: load failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取记忆失败"})
		return
	}
	pending, err := d.activateMemoriesPrompt(r.Context())
	if err != nil {
		slog.Error("agent memories: activate prompt failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法应用记忆"})
		return
	}
	writeJSON(w, http.StatusOK, memoriesResponse{Memories: snapshot.Memories, Revision: snapshot.Revision, PromptPending: pending})
}

func (d *AgentDeps) UpdateMemories(w http.ResponseWriter, r *http.Request) {
	if d.memories == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "记忆不可用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxMemoriesBytes+4096)
	var update memoriesUpdate
	if err := readJSON(r, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "记忆格式无效"})
		return
	}
	snapshot, err := d.memories.Save(update.Revision, update.Memories)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrMemoriesChanged):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "记忆已经发生变化，请重新加载后再编辑", "memories": snapshot.Memories, "revision": snapshot.Revision,
			})
		case errors.Is(err, store.ErrInvalidMemories):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "记忆内容无效"})
		default:
			slog.Error("agent memories: save failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存记忆失败"})
		}
		return
	}
	pending, activateErr := d.activateMemoriesPrompt(r.Context())
	if activateErr != nil {
		slog.Error("agent memories: activate saved prompt failed", "error", activateErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "记忆已保存，但暂时无法应用"})
		return
	}
	writeJSON(w, http.StatusOK, memoriesResponse{Memories: snapshot.Memories, Revision: snapshot.Revision, PromptPending: pending})
}

func (d *AgentDeps) activateMemoriesPrompt(ctx context.Context) (bool, error) {
	if d.settingsStore == nil || d.globalPromptFile == nil {
		return false, nil
	}
	return d.activateSavedGlobalPrompt(ctx)
}
