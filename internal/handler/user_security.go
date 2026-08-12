package handler

import (
	"errors"
	"net/http"
	"time"

	"marvo/internal/control"
)

func (d *Dependencies) GetUserSecurity(w http.ResponseWriter, r *http.Request) {
	user, err := d.Control.GetUser(r.Context(), d.UserID)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to load security settings"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"security": map[string]any{"totp_configured": user.TOTPConfigured},
	})
}

func (d *Dependencies) ChangeUserPassword(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("user-password-change:"+d.UserID, r, 10, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	nonce, err := generateChallenge()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to change password"})
		return
	}
	user, err := d.Control.ChangeUserPassword(r.Context(), d.UserID, body.CurrentPassword, body.NewPassword)
	if errors.Is(err, control.ErrInvalidUserCredentials) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "current password is invalid"})
		return
	}
	if errors.Is(err, control.ErrUserDisabled) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
		return
	}
	if err != nil {
		if validationErr := control.ValidatePassword(body.NewPassword); validationErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": validationErr.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to change password"})
		return
	}
	d.resetAttempts("user-password-change:"+d.UserID, r)
	setUserAdminCookie(w, r, user, nonce, d.Config.Server.SessionSecret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Dependencies) BeginUserTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("user-totp-enrollment:"+d.UserID, r, 10, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	enrollment, err := d.Control.BeginUserTOTPEnrollment(r.Context(), d.UserID, body.Password)
	if errors.Is(err, control.ErrInvalidUserCredentials) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "current password is invalid"})
		return
	}
	if errors.Is(err, control.ErrTOTPAlreadyConfigured) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TOTP is already configured"})
		return
	}
	if errors.Is(err, control.ErrUserDisabled) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to start TOTP enrollment"})
		return
	}
	d.resetAttempts("user-totp-enrollment:"+d.UserID, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"totp_setup": map[string]string{"secret": enrollment.TOTPSecret, "uri": enrollment.TOTPURI},
	})
}

func (d *Dependencies) ConfirmUserTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	nonce, err := generateChallenge()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to confirm TOTP enrollment"})
		return
	}
	user, err := d.Control.ConfirmUserTOTPEnrollment(r.Context(), d.UserID, body.Code, time.Now())
	if errors.Is(err, control.ErrTOTPInvalid) || errors.Is(err, control.ErrTOTPReplay) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid verification code"})
		return
	}
	if errors.Is(err, control.ErrTOTPNotConfigured) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TOTP enrollment has not started"})
		return
	}
	if errors.Is(err, control.ErrTOTPAlreadyConfigured) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TOTP is already configured"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to confirm TOTP enrollment"})
		return
	}
	setUserAdminCookie(w, r, user, nonce, d.Config.Server.SessionSecret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "security": map[string]any{"totp_configured": user.TOTPConfigured}})
}

func (d *Dependencies) DisableUserTOTP(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("user-totp-removal:"+d.UserID, r, 10, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	nonce, err := generateChallenge()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to remove TOTP"})
		return
	}
	user, err := d.Control.DisableUserTOTP(r.Context(), d.UserID, body.Password, body.Code, time.Now())
	if errors.Is(err, control.ErrInvalidUserCredentials) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "current password is invalid"})
		return
	}
	if errors.Is(err, control.ErrTOTPInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid verification code"})
		return
	}
	if errors.Is(err, control.ErrTOTPNotConfigured) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "TOTP is not configured"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to remove TOTP"})
		return
	}
	d.resetAttempts("user-totp-removal:"+d.UserID, r)
	setUserAdminCookie(w, r, user, nonce, d.Config.Server.SessionSecret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "security": map[string]any{"totp_configured": false}})
}
