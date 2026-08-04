package handler

import (
	"net/http"
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
