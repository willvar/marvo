package handler

import (
	"errors"
	"net/http"

	"marvo/internal/store"
)

func (d *Dependencies) GetBrand(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"brand": d.BrandStore.Get()})
}

func (d *Dependencies) UpdateBrand(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthJSONBytes)
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	brand, err := d.BrandStore.Save(body.Name)
	if errors.Is(err, store.ErrInvalidBrand) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to save brand"})
		return
	}
	if d.Hub != nil {
		d.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "brand_changed", "brand": brand}))
	}
	writeJSON(w, http.StatusOK, map[string]any{"brand": brand})
}
