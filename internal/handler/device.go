package handler

import (
	"errors"
	"marvo/internal/store"
	"net"
	"net/http"
	"strings"
	"time"
)

func (d *Dependencies) Apply(w http.ResponseWriter, r *http.Request) {
	if !d.allowAttempt("device-apply", r, 20, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many device applications"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		LocalDeviceID string           `json:"local_device_id"`
		DeviceName    string           `json:"device_name"`
		DeviceInfo    store.DeviceInfo `json:"device_info"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid request"})
		return
	}
	body.LocalDeviceID = strings.TrimSpace(body.LocalDeviceID)
	body.DeviceName = strings.TrimSpace(body.DeviceName)
	if body.LocalDeviceID == "" || len(body.LocalDeviceID) > 128 || len([]rune(body.DeviceName)) > 200 {
		writeJSON(w, 400, map[string]any{"error": "invalid request"})
		return
	}

	body.DeviceInfo.IPAddress = clientIP(r)

	req, createErr := d.DeviceStore.CreateRequest(body.LocalDeviceID, body.DeviceName, body.DeviceInfo)
	if createErr != nil {
		if errors.Is(createErr, store.ErrTooManyPendingDevices) {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many pending device applications"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to persist device application"})
		return
	}

	if dev, _ := d.DeviceStore.FindByLocalDeviceID(body.LocalDeviceID); dev != nil && req == nil {
		setDeviceCookie(w, r, dev.Token, d.DeviceStore.SignToken(dev.Token))
		writeJSON(w, 200, map[string]any{"status": "approved", "device_id": dev.ID})
		return
	}

	status := "pending"
	if req.ID == "" {
		status = "approved"
	}
	writeJSON(w, 200, map[string]any{
		"request_id": req.ID,
		"status":     status,
	})
}

func (d *Dependencies) Token(w http.ResponseWriter, r *http.Request) {
	localDeviceID := strings.TrimSpace(r.URL.Query().Get("local_device_id"))
	if localDeviceID == "" || len(localDeviceID) > 128 {
		writeJSON(w, 400, map[string]any{"error": "missing local_device_id"})
		return
	}

	dev, pending := d.DeviceStore.FindByLocalDeviceID(localDeviceID)
	if dev != nil {
		setDeviceCookie(w, r, dev.Token, d.DeviceStore.SignToken(dev.Token))
		writeJSON(w, 200, map[string]any{"status": "approved", "device_id": dev.ID})
		return
	}
	if pending != nil {
		writeJSON(w, 200, map[string]any{"status": "pending", "request_id": pending.ID})
		return
	}

	writeJSON(w, 200, map[string]any{"status": "not_found"})
}

func setDeviceCookie(w http.ResponseWriter, r *http.Request, token string, sig string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "marvo_device",
		Value:    token + ":" + sig,
		Path:     "/",
		MaxAge:   86400 * 365,
		Expires:  time.Now().Add(365 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		return ip
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "::1" {
		return "127.0.0.1"
	}
	return host
}
