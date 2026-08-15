package handler

import (
	"errors"
	"marvo/internal/store"
	"net/http"
	"strconv"
	"strings"
)

const maxActivityControlBody = 32 << 10

func (d *Dependencies) ListActivities(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Activity limit"})
			return
		}
		limit = parsed
	}
	page, err := d.Activity.List(limit, r.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrInvalidActivity) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Activity cursor"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list activities"})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (d *Dependencies) ActivityCounts(w http.ResponseWriter, _ *http.Request) {
	unread, pending, err := d.Activity.Counts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to count activities"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread, "pending": pending})
}

func (d *Dependencies) MarkActivitiesRead(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxActivityControlBody)
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	if err := d.Activity.MarkRead(body.IDs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Activity IDs"})
		return
	}
	d.broadcastActivityChanged()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Dependencies) DeleteActivity(w http.ResponseWriter, r *http.Request) {
	deleted, err := d.Activity.Delete(r.PathValue("id"))
	if errors.Is(err, store.ErrInvalidActivity) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Activity ID"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to delete Activity"})
		return
	}
	if deleted {
		d.broadcastActivityChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Dependencies) broadcastActivityChanged() {
	if d.Hub == nil || d.Activity == nil {
		return
	}
	unread, pending, err := d.Activity.Counts()
	if err != nil {
		return
	}
	d.Hub.BroadcastAll(store.MustJSON(map[string]any{
		"action": "activity_changed", "unread": unread, "pending": pending,
	}))
}
