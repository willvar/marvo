package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"marvo/internal/apprelease"
)

func TestAndroidReleasePublishAndPublicDownload(t *testing.T) {
	store, err := apprelease.Open(filepath.Join(t.TempDir(), "android"))
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{AppReleases: store}

	missing := httptest.NewRecorder()
	deps.GetAndroidRelease(missing, httptest.NewRequest(http.MethodGet, "/api/app/android/release", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing release status = %d, body = %s", missing.Code, missing.Body.String())
	}

	apk := handlerTestAPK(t, 12, "1.4.0")
	body, contentType := androidUpload(t, apk, "  新增离线前端  ", true)
	published := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/android/release", body)
	request.Header.Set("Content-Type", contentType)
	deps.PublishAndroidRelease(published, request)
	if published.Code != http.StatusOK {
		t.Fatalf("publish status = %d, body = %s", published.Code, published.Body.String())
	}
	var publishResponse struct {
		Release androidReleaseResponse `json:"release"`
		APKSize int64                  `json:"apk_size"`
	}
	if err := json.Unmarshal(published.Body.Bytes(), &publishResponse); err != nil {
		t.Fatal(err)
	}
	if publishResponse.Release.VersionCode != 12 || publishResponse.Release.VersionName != "1.4.0" ||
		publishResponse.Release.Message != "新增离线前端" || !publishResponse.Release.Required ||
		publishResponse.APKSize != int64(len(apk)) {
		t.Fatalf("publish response = %#v", publishResponse)
	}

	metadata := httptest.NewRecorder()
	deps.GetAndroidRelease(metadata, httptest.NewRequest(http.MethodGet, "/api/app/android/release", nil))
	if metadata.Code != http.StatusOK || metadata.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("metadata status = %d, headers = %#v", metadata.Code, metadata.Header())
	}
	if bytes.Contains(metadata.Body.Bytes(), []byte("sha256")) || bytes.Contains(metadata.Body.Bytes(), []byte("filename")) {
		t.Fatalf("public metadata leaked storage details: %s", metadata.Body.String())
	}

	download := httptest.NewRecorder()
	deps.DownloadAndroidAPK(download, httptest.NewRequest(http.MethodGet, "/api/app/android/apk", nil))
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), apk) {
		t.Fatalf("download status = %d, bytes = %d", download.Code, download.Body.Len())
	}
	if download.Header().Get("Content-Type") != "application/vnd.android.package-archive" ||
		!strings.Contains(download.Header().Get("Content-Disposition"), "Marvo-1.4.0.apk") ||
		download.Header().Get("ETag") == "" {
		t.Fatalf("download headers = %#v", download.Header())
	}
}

func TestAndroidReleasePublishRejectsInvalidUpload(t *testing.T) {
	store, err := apprelease.Open(filepath.Join(t.TempDir(), "android"))
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{AppReleases: store}
	body, contentType := androidUpload(t, []byte("not an APK"), "", false)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/admin/android/release", body)
	request.Header.Set("Content-Type", contentType)
	deps.PublishAndroidRelease(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid APK status = %d, body = %s", response.Code, response.Body.String())
	}
}

func androidUpload(t *testing.T, apk []byte, message string, required bool) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("apk", "Marvo.apk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(apk); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("message", message); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("required", stringValue(required)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func handlerTestAPK(t *testing.T, versionCode int64, versionName string) []byte {
	t.Helper()
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	entries := map[string][]byte{
		"AndroidManifest.xml": {0x03, 0x00, 0x08, 0x00},
		"classes.dex":         []byte("dex\n035\x00"),
		"assets/marvo-app.json": []byte(`{"application_id":"` + apprelease.ApplicationID + `","version_code":` +
			stringValue(versionCode) + `,"version_name":"` + versionName + `"}`),
	}
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func stringValue(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
