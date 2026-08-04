package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	challengeLifetime = 5 * time.Minute
	adminSessionAge   = 7 * 24 * time.Hour
	maxAuthJSONBytes  = 64 << 10
)

func (d *Dependencies) Verify(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("admin-verify", r, 10, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid request"})
		return
	}

	provided := sha256.Sum256([]byte(body.Password))
	expected := sha256.Sum256([]byte(d.Config.Auth.Password))
	if !hmac.Equal(provided[:], expected[:]) {
		writeJSON(w, 401, map[string]any{"error": "invalid password"})
		return
	}
	d.resetAttempts("admin-verify", r)

	challenge, err := generateChallenge()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create challenge"})
		return
	}
	expiry := time.Now().Add(challengeLifetime).Unix()
	d.rememberChallenge(challenge, expiry)

	payload := fmt.Sprintf("%s:%d", challenge, expiry)
	sig := signPayload(payload, d.Config.Server.SessionSecret)

	token := fmt.Sprintf("%s:%d:%s", challenge, expiry, sig)
	writeJSON(w, 200, map[string]any{"challenge_token": token})
}

func (d *Dependencies) Login(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("admin-login", r, 30, 5*time.Minute) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		ChallengeToken string `json:"challenge_token"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid request"})
		return
	}

	challenge, expiryStr, sig, err := parseChallengeToken(body.ChallengeToken)
	if err != nil {
		writeJSON(w, 401, map[string]any{"error": "invalid challenge token"})
		return
	}

	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		writeJSON(w, 401, map[string]any{"error": "challenge token expired"})
		return
	}

	expectedSig := signPayload(fmt.Sprintf("%s:%s", challenge, expiryStr), d.Config.Server.SessionSecret)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		writeJSON(w, 401, map[string]any{"error": "invalid challenge token"})
		return
	}
	if !d.consumeChallenge(challenge, expiry) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "challenge token already used or unknown"})
		return
	}
	d.resetAttempts("admin-login", r)

	sessionValue := fmt.Sprintf("marvo:%d:%s", time.Now().Unix(), challenge)
	sessionSig := signPayload(sessionValue, d.Config.Server.SessionSecret)
	sessionToken := fmt.Sprintf("%s:%s", sessionValue, sessionSig)

	http.SetCookie(w, &http.Cookie{
		Name:     "marvo_session",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(adminSessionAge.Seconds()),
		Expires:  time.Now().Add(adminSessionAge),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, 200, map[string]any{"ok": true})
}

func (d *Dependencies) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "marvo_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (d *Dependencies) AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.validateDeviceCookie(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, 401, map[string]any{"error": "unauthorized"})
		})
	}
}

func (d *Dependencies) AdminMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d.validateSessionCookie(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, 401, map[string]any{"error": "unauthorized"})
		})
	}
}

func (d *Dependencies) validateSessionCookie(r *http.Request) bool {
	cookie, err := r.Cookie("marvo_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	value, sig, found := cutLast(cookie.Value, ":")
	if !found {
		return false
	}
	expectedSig := signPayload(value, d.Config.Server.SessionSecret)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "marvo" || parts[2] == "" {
		return false
	}
	issuedAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	now := time.Now()
	issued := time.Unix(issuedAt, 0)
	return !issued.After(now.Add(time.Minute)) && now.Sub(issued) <= adminSessionAge
}

func (d *Dependencies) validateDeviceCookie(r *http.Request) bool {
	cookie, err := r.Cookie("marvo_device")
	if err != nil || cookie.Value == "" {
		return false
	}
	token, sig, found := cutLast(cookie.Value, ":")
	if !found {
		return false
	}
	return d.DeviceStore.VerifyToken(token, sig)
}

func generateChallenge() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func signPayload(payload string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func parseChallengeToken(token string) (challenge, expiry, sig string, err error) {
	parts := strings.SplitN(token, ":", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid token format")
	}
	return parts[0], parts[1], parts[2], nil
}

func cutLast(s string, sep string) (before string, after string, found bool) {
	if idx := strings.LastIndex(s, sep); idx >= 0 {
		return s[:idx], s[idx+len(sep):], true
	}
	return s, "", false
}
