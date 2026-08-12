package handler

import "net/http"

func (d *Dependencies) GetSpaceInfo(w http.ResponseWriter, _ *http.Request) {
	usedBytes, err := d.Layout.UserUsage(d.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to calculate space usage"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"space": map[string]any{
			"used_bytes":     usedBytes,
			"capacity_bytes": nil,
		},
	})
}
