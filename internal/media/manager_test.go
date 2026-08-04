package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marvo/internal/store"
)

const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func waitForAssetState(t *testing.T, manager *Manager, title, token, id string, want State, timeout time.Duration) Asset {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *Asset
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = manager.Get(title, token, id)
		if lastErr == nil && last.State == want {
			return *last
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("asset state = %s (%s), want %s", last.State, last.Error, want)
	}
	t.Fatalf("asset lookup error = %v, want state %s", lastErr, want)
	return Asset{}
}

func createMediaNote(t *testing.T, content string) (*store.NoteStore, *store.NoteSnapshot) {
	t.Helper()
	noteStore := store.NewNoteStore(t.TempDir())
	snapshot, err := noteStore.CreateNote("media-note", content, nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}
	return noteStore, snapshot
}

func TestNewAssetIDIsUUIDv4(t *testing.T) {
	for range 32 {
		id, err := NewAssetID()
		if err != nil {
			t.Fatalf("NewAssetID() error = %v", err)
		}
		if !ValidAssetID(id) {
			t.Fatalf("NewAssetID() = %q, not a UUIDv4", id)
		}
	}
}

func TestCompatibleImageUploadBecomesPrivateReadyAsset(t *testing.T) {
	id, err := NewAssetID()
	if err != nil {
		t.Fatal(err)
	}
	noteStore, note := createMediaNote(t, "![image](asset:"+id+")")
	manager := NewManager(noteStore)
	defer manager.Close()
	if _, err := manager.Reserve(note.Note.Title, note.InstanceToken, id, "pixel.png", "image/png"); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	png, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(context.Background(), note.Note.Title, note.InstanceToken, id, int64(len(png)), bytes.NewReader(png)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	ready := waitForAssetState(t, manager, note.Note.Title, note.InstanceToken, id, StateReady, 5*time.Second)
	if ready.Filename != id+".png" || ready.Kind != "image" {
		t.Fatalf("ready asset = %#v", ready)
	}

	assetsDir, err := noteStore.AssetsDirCAS(note.Note.Title, note.InstanceToken)
	if err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(assetsDir, ready.Filename)
	info, err := os.Stat(finalPath)
	if err != nil {
		t.Fatalf("Stat(final asset) error = %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("final asset permissions = %04o, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(uploadPath(assetsDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upload original remains after ready state: %v", err)
	}
}

func TestUnreferencedPlaceholderAbandonsAndRemovesAllFiles(t *testing.T) {
	id, err := NewAssetID()
	if err != nil {
		t.Fatal(err)
	}
	noteStore, note := createMediaNote(t, "asset:"+id)
	manager := NewManager(noteStore)
	defer manager.Close()
	if _, err := manager.Reserve(note.Note.Title, note.InstanceToken, id, "unused.png", "image/png"); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := noteStore.UpdateContentCAS(note.Note.Title, note.InstanceToken, note.ContentRevision, "placeholder removed"); err != nil {
		t.Fatalf("UpdateContentCAS() error = %v", err)
	}
	abandoned, err := manager.Abandon(note.Note.Title, note.InstanceToken, id)
	if err != nil || abandoned.State != StateAbandoned {
		t.Fatalf("Abandon() = %#v, %v", abandoned, err)
	}
	if _, err := manager.Get(note.Note.Title, note.InstanceToken, id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Get() after abandon error = %v, want not exist", err)
	}
	assetsDir, err := noteStore.AssetsDirCAS(note.Note.Title, note.InstanceToken)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), id) {
			t.Fatalf("abandoned asset file remains: %s", entry.Name())
		}
	}
	if manager.HasBusyAssets(note.Note.Title, note.InstanceToken) {
		t.Fatal("abandoned placeholder is still considered busy")
	}
}

func TestCorruptUploadFailsAndDropsRawUpload(t *testing.T) {
	id, err := NewAssetID()
	if err != nil {
		t.Fatal(err)
	}
	noteStore, note := createMediaNote(t, "asset:"+id)
	manager := NewManager(noteStore)
	defer manager.Close()
	if _, err := manager.Reserve(note.Note.Title, note.InstanceToken, id, "broken.png", "image/png"); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	corrupt := []byte("this is not a png")
	if _, err := manager.Upload(context.Background(), note.Note.Title, note.InstanceToken, id, int64(len(corrupt)), bytes.NewReader(corrupt)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	failed := waitForAssetState(t, manager, note.Note.Title, note.InstanceToken, id, StateFailed, 5*time.Second)
	if failed.Error == "" || failed.Filename != "" {
		t.Fatalf("failed asset = %#v", failed)
	}
	assetsDir, err := noteStore.AssetsDirCAS(note.Note.Title, note.InstanceToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(uploadPath(assetsDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt raw upload remains: %v", err)
	}
}

func TestHEVCMOVIsTranscodedToChromiumCompatibleMP4(t *testing.T) {
	ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
	ffprobe, ffprobeErr := exec.LookPath("ffprobe")
	if ffmpegErr != nil || ffprobeErr != nil {
		t.Skip("ffmpeg and ffprobe are required for the transcode integration test")
	}
	fixtureDir := t.TempDir()
	fixture := filepath.Join(fixtureDir, "iphone.mov")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg,
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=1", "-t", "1", "-an",
		"-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=error:pools=1:frame-threads=1",
		"-tag:v", "hvc1", "-y", fixture,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("local ffmpeg cannot create HEVC/MOV fixture: %v (%s)", err, output)
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewAssetID()
	if err != nil {
		t.Fatal(err)
	}
	noteStore, note := createMediaNote(t, "asset:"+id)
	manager := NewManager(noteStore)
	defer manager.Close()
	manager.ffmpeg = ffmpeg
	manager.ffprobe = ffprobe
	if _, err := manager.Reserve(note.Note.Title, note.InstanceToken, id, "iphone.mov", "video/quicktime"); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := manager.Upload(context.Background(), note.Note.Title, note.InstanceToken, id, int64(len(raw)), bytes.NewReader(raw)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	ready := waitForAssetState(t, manager, note.Note.Title, note.InstanceToken, id, StateReady, 20*time.Second)
	if ready.Filename != id+".mp4" {
		t.Fatalf("transcoded filename = %q, want UUID.mp4", ready.Filename)
	}
	assetsDir, err := noteStore.AssetsDirCAS(note.Note.Title, note.InstanceToken)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := manager.probe(context.Background(), filepath.Join(assetsDir, ready.Filename))
	if err != nil || !compatibleVideo(".mp4", probe) {
		t.Fatalf("transcoded output is not Chromium-compatible: probe=%#v, error=%v", probe, err)
	}
	if _, err := os.Stat(uploadPath(assetsDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("MOV original remains after transcode: %v", err)
	}
}

func TestHEICIsTranscodedToJPEGWithoutKeepingOriginal(t *testing.T) {
	ffmpeg, ffmpegErr := exec.LookPath("ffmpeg")
	heifEncoder, encoderErr := exec.LookPath("heif-enc")
	if ffmpegErr != nil || encoderErr != nil {
		t.Skip("ffmpeg and heif-enc are required for the HEIC integration test")
	}
	fixtureDir := t.TempDir()
	pngPath := filepath.Join(fixtureDir, "source.png")
	pngFile, err := os.OpenFile(pngPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	sourceImage := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			sourceImage.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 80, A: 255})
		}
	}
	if err := png.Encode(pngFile, sourceImage); err != nil {
		_ = pngFile.Close()
		t.Fatal(err)
	}
	if err := pngFile.Close(); err != nil {
		t.Fatal(err)
	}
	heicPath := filepath.Join(fixtureDir, "iphone.heic")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, heifEncoder, "-q", "60", "-o", heicPath, pngPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("local heif-enc cannot create HEIC fixture: %v (%s)", err, output)
	}
	raw, err := os.ReadFile(heicPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewAssetID()
	if err != nil {
		t.Fatal(err)
	}
	noteStore, note := createMediaNote(t, "asset:"+id)
	manager := NewManager(noteStore)
	defer manager.Close()
	manager.ffmpeg = ffmpeg
	if _, err := manager.Reserve(note.Note.Title, note.InstanceToken, id, "iphone.heic", "image/heic"); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := manager.Upload(context.Background(), note.Note.Title, note.InstanceToken, id, int64(len(raw)), bytes.NewReader(raw)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	ready := waitForAssetState(t, manager, note.Note.Title, note.InstanceToken, id, StateReady, 20*time.Second)
	if ready.Filename != id+".jpg" {
		t.Fatalf("transcoded filename = %q, want UUID.jpg", ready.Filename)
	}
	assetsDir, err := noteStore.AssetsDirCAS(note.Note.Title, note.InstanceToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCompatibleImage(filepath.Join(assetsDir, ready.Filename), ".jpg"); err != nil {
		t.Fatalf("transcoded JPEG is invalid: %v", err)
	}
	if _, err := os.Stat(uploadPath(assetsDir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("HEIC original remains after transcode: %v", err)
	}
}
