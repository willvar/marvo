package handler

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"marvo/internal/control"
)

func (d *Dependencies) GetUserIdentity(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if !control.ValidateUserID(userID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	user, err := d.Control.GetUser(r.Context(), userID)
	if errors.Is(err, control.ErrUserNotFound) || (err == nil && user.Status == control.UserStatusDisabled) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to load user"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name})
}

func (d *Dependencies) VerifyUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if !control.ValidateUserID(userID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	rateKind := "user-verify:" + userID
	if !d.allowAttempt(rateKind, r, 10, 5*time.Minute) {
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
	login, err := d.Control.BeginUserLogin(r.Context(), userID, body.Password)
	if errors.Is(err, control.ErrInvalidUserCredentials) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid password"})
		return
	}
	if errors.Is(err, control.ErrUserDisabled) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to verify user"})
		return
	}
	d.resetAttempts(rateKind, r)

	if !login.User.TOTPConfigured {
		nonce, err := generateChallenge()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create session"})
			return
		}
		setUserAdminCookie(w, r, &login.User, nonce, d.Config.Server.SessionSecret)
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"user":          login.User,
		})
		return
	}

	challenge, err := generateChallenge()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create challenge"})
		return
	}
	expiry := time.Now().Add(challengeLifetime).Unix()
	d.rememberChallenge(challenge, expiry)
	token := signUserChallenge(userID, login.User.AuthVersion, challenge, expiry, d.Config.Server.SessionSecret)
	response := map[string]any{
		"authenticated":   false,
		"challenge_token": token,
		"user":            login.User,
	}
	writeJSON(w, http.StatusOK, response)
}

func (d *Dependencies) LoginUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	rateKind := "user-login:" + userID
	if !control.ValidateUserID(userID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if !d.allowAttempt(rateKind, r, 20, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		ChallengeToken string `json:"challenge_token"`
		Code           string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	tokenUserID, authVersion, challenge, expiry, ok := parseAndVerifyUserChallenge(body.ChallengeToken, d.Config.Server.SessionSecret)
	if !ok || tokenUserID != userID || time.Now().Unix() > expiry || !d.hasChallenge(challenge, expiry) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid challenge token"})
		return
	}
	current, err := d.Control.GetUser(r.Context(), userID)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to load user"})
		return
	}
	if current.Status == control.UserStatusDisabled {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
		return
	}
	if !current.TOTPConfigured {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "TOTP is not configured"})
		return
	}
	if current.AuthVersion != authVersion {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "challenge token expired"})
		return
	}
	verified, err := d.Control.VerifyUserTOTP(r.Context(), userID, body.Code, time.Now())
	if errors.Is(err, control.ErrTOTPInvalid) || errors.Is(err, control.ErrTOTPReplay) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid verification code"})
		return
	}
	if errors.Is(err, control.ErrUserDisabled) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user disabled"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to verify code"})
		return
	}
	if !d.consumeChallenge(challenge, expiry) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "challenge token already used"})
		return
	}
	d.resetAttempts(rateKind, r)
	setUserAdminCookie(w, r, verified, challenge, d.Config.Server.SessionSecret)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": verified})
}

func (d *Dependencies) LogoutUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if !control.ValidateUserID(userID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     userAdminCookieName(userID),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Dependencies) GetUserAdminIdentity(w http.ResponseWriter, r *http.Request) {
	user, err := d.Control.GetUser(r.Context(), d.UserID)
	if errors.Is(err, control.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to load user"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (d *Dependencies) UserAdminMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.PathValue("userID")
			if d.validateUserAdminCookie(r, userID) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		})
	}
}

func (d *Dependencies) validateUserAdminCookie(r *http.Request, userID string) bool {
	if !control.ValidateUserID(userID) {
		return false
	}
	cookie, err := r.Cookie(userAdminCookieName(userID))
	if err != nil || cookie.Value == "" {
		return false
	}
	value, signature, found := cutLast(cookie.Value, ":")
	if !found || !hmac.Equal([]byte(signature), []byte(signPayload(value, d.Config.Server.SessionSecret))) {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) != 5 || parts[0] != "marvo-user" || parts[1] != userID || parts[4] == "" {
		return false
	}
	authVersion, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return false
	}
	issuedAt, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return false
	}
	now := time.Now()
	issued := time.Unix(issuedAt, 0)
	if issued.After(now.Add(time.Minute)) || now.Sub(issued) > adminSessionAge {
		return false
	}
	user, err := d.Control.GetUser(r.Context(), userID)
	return err == nil && user.Status == control.UserStatusActive && user.AuthVersion == authVersion
}

func signUserChallenge(userID string, authVersion int64, challenge string, expiry int64, secret string) string {
	payload := fmt.Sprintf("%s:%d:%s:%d", userID, authVersion, challenge, expiry)
	return payload + ":" + signPayload(payload, secret)
}

func parseAndVerifyUserChallenge(token, secret string) (userID string, authVersion int64, challenge string, expiry int64, ok bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 5 || !control.ValidateUserID(parts[0]) || parts[2] == "" {
		return "", 0, "", 0, false
	}
	payload := strings.Join(parts[:4], ":")
	if !hmac.Equal([]byte(parts[4]), []byte(signPayload(payload, secret))) {
		return "", 0, "", 0, false
	}
	authVersion, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || authVersion < 1 {
		return "", 0, "", 0, false
	}
	expiry, err = strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return "", 0, "", 0, false
	}
	return parts[0], authVersion, parts[2], expiry, true
}

func setUserAdminCookie(w http.ResponseWriter, r *http.Request, user *control.User, challenge, secret string) {
	value := fmt.Sprintf("marvo-user:%s:%d:%d:%s", user.ID, user.AuthVersion, time.Now().Unix(), challenge)
	http.SetCookie(w, &http.Cookie{
		Name:     userAdminCookieName(user.ID),
		Value:    value + ":" + signPayload(value, secret),
		Path:     "/",
		MaxAge:   int(adminSessionAge.Seconds()),
		Expires:  time.Now().Add(adminSessionAge),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func userAdminCookieName(userID string) string {
	return "marvo_user_session_" + userID
}
