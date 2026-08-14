package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"marvo/internal/apprelease"
)

const androidReleaseFormOverhead = int64(1 << 20)

type androidReleaseResponse struct {
	VersionCode int64  `json:"version_code"`
	VersionName string `json:"version_name"`
	Required    bool   `json:"required"`
	Message     string `json:"message"`
}

func (d *Dependencies) GetAndroidRelease(w http.ResponseWriter, _ *http.Request) {
	release, err := d.currentAndroidRelease()
	if errors.Is(err, apprelease.ErrNoRelease) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Android APP is not published"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to load Android release"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, publicAndroidRelease(release))
}

func (d *Dependencies) DownloadAndroidAPK(w http.ResponseWriter, r *http.Request) {
	if d.AppReleases == nil {
		http.NotFound(w, r)
		return
	}
	file, release, err := d.AppReleases.OpenAPK()
	if errors.Is(err, apprelease.ErrNoRelease) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="Marvo-%s.apk"`, release.VersionName))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", `"`+release.SHA256+`"`)
	http.ServeContent(w, r, "Marvo.apk", release.PublishedAt, file)
}

func (d *Dependencies) PublishAndroidRelease(w http.ResponseWriter, r *http.Request) {
	if d.AppReleases == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Android release storage is unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, apprelease.MaxAPKBytes+androidReleaseFormOverhead)
	if err := r.ParseMultipartForm(androidReleaseFormOverhead); err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "APK exceeds the upload limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid Android release upload"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	required, err := parseRequiredField(r.FormValue("required"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "required must be true or false"})
		return
	}
	file, header, err := r.FormFile("apk")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "APK file is required"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid APK file"})
		return
	}
	defer file.Close()
	if header.Size > apprelease.MaxAPKBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "APK exceeds the upload limit"})
		return
	}
	release, err := d.AppReleases.Publish(file, r.FormValue("message"), required)
	switch {
	case errors.Is(err, apprelease.ErrAPKTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "APK exceeds the upload limit"})
	case errors.Is(err, apprelease.ErrInvalidAPK):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "file is not a Marvo Android APK"})
	case errors.Is(err, apprelease.ErrInvalidMessage):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "release message is too long"})
	case errors.Is(err, apprelease.ErrVersionNotNewer):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "APK version code must be greater than the published version"})
	case err != nil:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "failed to publish Android release"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"release":      publicAndroidRelease(release),
			"published_at": release.PublishedAt,
			"apk_size":     release.APKSize,
		})
	}
}

func (d *Dependencies) currentAndroidRelease() (*apprelease.Release, error) {
	if d.AppReleases == nil {
		return nil, apprelease.ErrNoRelease
	}
	return d.AppReleases.Current()
}

func publicAndroidRelease(release *apprelease.Release) androidReleaseResponse {
	return androidReleaseResponse{
		VersionCode: release.VersionCode,
		VersionName: release.VersionName,
		Required:    release.Required,
		Message:     release.Message,
	}
}

func parseRequiredField(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}
