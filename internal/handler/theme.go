package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type ThemeConfig struct {
	FontFamily        string      `json:"fontFamily,omitempty"`
	FontSize          json.Number `json:"fontSize,omitempty"`
	DarkMode          interface{} `json:"darkMode,omitempty"`
	ContentFontSize   json.Number `json:"contentFontSize,omitempty"`
	ContentLineHeight json.Number `json:"contentLineHeight,omitempty"`
	ContentWidth      interface{} `json:"contentWidth,omitempty"`
	AccentColor       string      `json:"accentColor,omitempty"`
	Radius            json.Number `json:"radius,omitempty"`
}

const defaultThemeJSON = `{
  "fontFamily": "-apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, \"Noto Sans SC\", sans-serif",
  "fontSize": 14,
  "darkMode": "system",
  "contentFontSize": 15,
  "contentLineHeight": 1.8,
  "contentWidth": "full",
  "accentColor": "#4f46e5",
  "radius": 8
}
`

func EnsureThemeFile(dataDir string) error {
	path := filepath.Join(dataDir, "theme.json")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("theme.json is not a regular file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeThemeFileAtomic(path, []byte(defaultThemeJSON))
}

func (d *Dependencies) GetTheme(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(d.Config.Server.DataDir, "theme.json")
	info, statErr := os.Lstat(path)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64<<10) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid theme.json"})
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, map[string]any{})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "failed to read theme"})
		return
	}

	var theme ThemeConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&theme); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid theme.json"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid theme.json"})
		return
	}

	writeJSON(w, 200, theme)
}

func writeThemeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".theme-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
