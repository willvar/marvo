package apprelease

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStorePublishesAndReloadsAndroidRelease(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "android")
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Current(); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Current() error = %v, want ErrNoRelease", err)
	}

	release, err := store.Publish(bytes.NewReader(testAPK(t, ApplicationID, 7, "1.2.0")), " 更新说明 ", true)
	if err != nil {
		t.Fatal(err)
	}
	if release.VersionCode != 7 || release.VersionName != "1.2.0" || release.Message != "更新说明" || !release.Required {
		t.Fatalf("release = %#v", release)
	}
	if release.APKSize <= 0 || len(release.SHA256) != 64 || release.Filename == "" {
		t.Fatalf("release artifact metadata = %#v", release)
	}
	info, err := os.Stat(filepath.Join(directory, release.Filename))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("published APK info = %#v, error = %v", info, err)
	}

	reloaded, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reloaded.Current()
	if err != nil || current.VersionCode != 7 || current.Filename != release.Filename {
		t.Fatalf("reloaded release = %#v, error = %v", current, err)
	}
	file, opened, err := reloaded.OpenAPK()
	if err != nil {
		t.Fatal(err)
	}
	if opened.SHA256 != release.SHA256 {
		t.Fatalf("opened release = %#v", opened)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	publishedPath := filepath.Join(directory, release.Filename)
	tampered, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered[0] ^= 0xff
	if err := os.WriteFile(publishedPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	broken, err := Open(directory)
	if err == nil || broken == nil {
		t.Fatal("tampered APK was accepted when reloading the release store")
	}
	if _, err := broken.Publish(bytes.NewReader(testAPK(t, ApplicationID, 8, "1.3.0")), "", false); err != nil {
		t.Fatalf("replace tampered release: %v", err)
	}
}

func TestStoreRejectsInvalidAndNonIncreasingAPKs(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "android"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(bytes.NewReader([]byte("not an APK")), "", false); !errors.Is(err, ErrInvalidAPK) {
		t.Fatalf("invalid APK error = %v", err)
	}
	if _, err := store.Publish(bytes.NewReader(testAPK(t, "example.invalid", 1, "1.0.0")), "", false); !errors.Is(err, ErrInvalidAPK) {
		t.Fatalf("wrong application error = %v", err)
	}
	if _, err := store.Publish(bytes.NewReader(testAPK(t, ApplicationID, 2, "1.0.0")), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(bytes.NewReader(testAPK(t, ApplicationID, 2, "1.0.1")), "", false); !errors.Is(err, ErrVersionNotNewer) {
		t.Fatalf("same version error = %v", err)
	}
}

func testAPK(t *testing.T, applicationID string, versionCode int64, versionName string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range map[string][]byte{
		"AndroidManifest.xml": {0x03, 0x00, 0x08, 0x00},
		"classes.dex":         []byte("dex\n035\x00"),
		metadataPath: mustJSON(t, apkMetadata{
			ApplicationID: applicationID,
			VersionCode:   versionCode,
			VersionName:   versionName,
		}),
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
