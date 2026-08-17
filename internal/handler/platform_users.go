package handler

import (
	"errors"
	"net/http"
	"strings"

	"marvo/internal/control"
)

func (d *Dependencies) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Control.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list users"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (d *Dependencies) CreateUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	enrollment, err := d.Control.CreateUser(r.Context(), strings.TrimSpace(body.Name), body.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if _, err := d.Layout.EnsureUser(enrollment.User.ID); err != nil {
		_ = d.Control.DeleteSetupUser(r.Context(), enrollment.User.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to initialize user space"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":          enrollment.User,
		"workspace_url": "/user/" + enrollment.User.ID,
	})
}

func (d *Dependencies) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	if err := readJSON(r, &body); err != nil || body.Disabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	user, err := d.Control.SetUserDisabled(r.Context(), userID, *body.Disabled)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to update user"})
		return
	}
	if *body.Disabled && d.Spaces != nil {
		d.Spaces.StopUserSchedules(userID)
		d.Spaces.CloseUser(userID)
	} else if d.Spaces != nil {
		d.Spaces.WakeSchedules(userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (d *Dependencies) ResetUserCredentials(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	enrollment, err := d.Control.ResetUserCredentials(r.Context(), userID, body.Password)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": enrollment.User})
}
