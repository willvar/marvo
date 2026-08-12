package handler

import (
	"errors"
	"net/http"
	"strings"

	"marvo/internal/store"
)

func (d *Dependencies) ListRequests(w http.ResponseWriter, r *http.Request) {
	reqs := d.DeviceStore.ListRequests()
	writeJSON(w, 200, map[string]any{"requests": reqs})
}

func (d *Dependencies) ApproveRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, 400, map[string]any{"error": "missing request id"})
		return
	}

	dev, err := d.DeviceStore.ApproveRequest(id)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to persist approval"})
		return
	}
	if dev == nil {
		writeJSON(w, 404, map[string]any{"error": "request not found"})
		return
	}

	writeJSON(w, 200, map[string]any{"device": dev})
}

func (d *Dependencies) RejectRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, 400, map[string]any{"error": "missing request id"})
		return
	}

	removed, err := d.DeviceStore.RejectRequest(id)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to persist rejection"})
		return
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "request not found"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (d *Dependencies) ListDevices(w http.ResponseWriter, r *http.Request) {
	devs := d.DeviceStore.ListDevices()
	writeJSON(w, 200, map[string]any{"devices": devs})
}

func (d *Dependencies) RenameDevice(w http.ResponseWriter, r *http.Request) {
	localDeviceID := strings.TrimSpace(r.PathValue("id"))
	if localDeviceID == "" || len(localDeviceID) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing device id"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		DeviceName string `json:"device_name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}

	device, err := d.DeviceStore.RenameDevice(localDeviceID, body.DeviceName)
	switch {
	case errors.Is(err, store.ErrInvalidDeviceName):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "device name must contain 1 to 50 characters"})
		return
	case errors.Is(err, store.ErrDeviceNameConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "device name already exists"})
		return
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to persist device name"})
		return
	case device == nil:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "device not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"device": device})
}

func (d *Dependencies) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, 400, map[string]any{"error": "missing device id"})
		return
	}

	revoked, err := d.DeviceStore.RevokeDevice(id)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to persist revocation"})
		return
	}
	if !revoked {
		writeJSON(w, 404, map[string]any{"error": "device not found"})
		return
	}

	writeJSON(w, 200, map[string]any{"ok": true})
}
