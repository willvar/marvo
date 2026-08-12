package handler

import (
	"errors"
	"net/http"

	"marvo/internal/control"
	"marvo/internal/userspace"
)

func (d *Dependencies) LegacyMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := d.Layout.InspectLegacy(d.Legacy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to inspect legacy data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"legacy": status})
}

func (d *Dependencies) MigrateLegacyUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	user, err := d.Control.GetUser(r.Context(), userID)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to load user"})
		return
	}
	if user.Status == control.UserStatusDisabled {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "disabled user cannot receive legacy data"})
		return
	}

	d.migrationMu.Lock()
	defer d.migrationMu.Unlock()
	release, err := d.Spaces.BeginMigration(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "user space is busy"})
		return
	}
	defer release()
	result, err := d.Layout.MigrateLegacy(userID, d.Legacy)
	switch {
	case errors.Is(err, userspace.ErrLegacyUnavailable):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "legacy data not found"})
	case errors.Is(err, userspace.ErrMigrationConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "target user contains conflicting data"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "legacy migration failed"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"migration": result})
	}
}
