package apprelease

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ApplicationID   = "cn.willvar.marvo"
	MaxAPKBytes     = int64(256 << 20)
	MaxMessageRunes = 2000
	metadataPath    = "assets/marvo-app.json"
	releaseFile     = "release.json"
)

var (
	ErrNoRelease       = errors.New("android release is not published")
	ErrInvalidAPK      = errors.New("invalid Marvo APK")
	ErrAPKTooLarge     = errors.New("APK exceeds the upload limit")
	ErrVersionNotNewer = errors.New("APK version code must increase")
	ErrInvalidMessage  = errors.New("invalid Android release message")
	versionNamePattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}(?:[-+][0-9A-Za-z.-]+)?$`)
)

type Release struct {
	VersionCode int64     `json:"version_code"`
	VersionName string    `json:"version_name"`
	Required    bool      `json:"required"`
	Message     string    `json:"message"`
	PublishedAt time.Time `json:"published_at"`
	APKSize     int64     `json:"apk_size"`
	SHA256      string    `json:"sha256,omitempty"`
	Filename    string    `json:"-"`
}

type releaseRecord struct {
	Release
	Filename string `json:"filename"`
}

type apkMetadata struct {
	ApplicationID string `json:"application_id"`
	VersionCode   int64  `json:"version_code"`
	VersionName   string `json:"version_name"`
}

type Store struct {
	directory string
	mu        sync.RWMutex
	current   *Release
}

func Open(directory string) (*Store, error) {
	directory = filepath.Clean(directory)
	if !filepath.IsAbs(directory) {
		return nil, errors.New("android release directory must be absolute")
	}
	if err := ensureDirectory(directory); err != nil {
		return nil, fmt.Errorf("initialize Android release directory: %w", err)
	}
	store := &Store{directory: directory}
	current, err := store.load()
	if err != nil && !errors.Is(err, ErrNoRelease) {
		return store, err
	}
	store.current = current
	return store, nil
}

func (s *Store) Current() (*Release, error) {
	if s == nil {
		return nil, ErrNoRelease
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, ErrNoRelease
	}
	copy := *s.current
	return &copy, nil
}

func (s *Store) Publish(source io.Reader, message string, required bool) (*Release, error) {
	if s == nil || source == nil {
		return nil, ErrInvalidAPK
	}
	message = strings.TrimSpace(message)
	if !utf8.ValidString(message) || len([]rune(message)) > MaxMessageRunes {
		return nil, ErrInvalidMessage
	}

	temporary, err := os.CreateTemp(s.directory, ".marvo-android-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create APK staging file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return nil, fmt.Errorf("protect APK staging file: %w", err)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, MaxAPKBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stage APK: %w", err)
	}
	if written > MaxAPKBytes {
		return nil, ErrAPKTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return nil, fmt.Errorf("sync APK staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close APK staging file: %w", err)
	}

	metadata, err := inspectAPK(temporaryPath)
	if err != nil {
		return nil, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	release := &Release{
		VersionCode: metadata.VersionCode,
		VersionName: metadata.VersionName,
		Required:    required,
		Message:     message,
		PublishedAt: time.Now().UTC(),
		APKSize:     written,
		SHA256:      digest,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && release.VersionCode <= s.current.VersionCode {
		return nil, ErrVersionNotNewer
	}
	filename := fmt.Sprintf("marvo-%d-%s.apk", release.VersionCode, digest[:12])
	finalPath := filepath.Join(s.directory, filename)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return nil, fmt.Errorf("publish APK file: %w", err)
	}
	keepTemporary = true
	release.Filename = filename
	if err := writeReleaseRecordAtomic(s.directory, release); err != nil {
		_ = os.Remove(finalPath)
		return nil, err
	}
	if err := syncDirectory(s.directory); err != nil {
		return nil, fmt.Errorf("sync Android release directory: %w", err)
	}
	s.current = release
	copy := *release
	return &copy, nil
}

func (s *Store) OpenAPK() (*os.File, *Release, error) {
	if s == nil {
		return nil, nil, ErrNoRelease
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, nil, ErrNoRelease
	}
	file, err := openRegularFile(filepath.Join(s.directory, s.current.Filename))
	if err != nil {
		return nil, nil, fmt.Errorf("open published APK: %w", err)
	}
	copy := *s.current
	return file, &copy, nil
}

func (s *Store) load() (*Release, error) {
	path := filepath.Join(s.directory, releaseFile)
	file, err := openRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoRelease
	}
	if err != nil {
		return nil, fmt.Errorf("open Android release metadata: %w", err)
	}
	defer file.Close()
	var record releaseRecord
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("decode Android release metadata: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("android release metadata contains trailing data")
	}
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	apk, err := openRegularFile(filepath.Join(s.directory, record.Filename))
	if err != nil {
		return nil, fmt.Errorf("open Android release APK: %w", err)
	}
	info, statErr := apk.Stat()
	if statErr != nil || info.Size() != record.APKSize {
		_ = apk.Close()
		return nil, errors.New("android release APK metadata does not match the file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, apk); err != nil {
		_ = apk.Close()
		return nil, fmt.Errorf("verify Android release APK: %w", err)
	}
	if err := apk.Close(); err != nil {
		return nil, fmt.Errorf("close Android release APK: %w", err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
		return nil, errors.New("android release APK checksum does not match the metadata")
	}
	release := record.Release
	release.Filename = record.Filename
	return &release, nil
}

func validateRecord(record releaseRecord) error {
	if record.VersionCode <= 0 || !versionNamePattern.MatchString(record.VersionName) || record.APKSize <= 0 {
		return errors.New("android release metadata is invalid")
	}
	if len(record.SHA256) != sha256.Size*2 {
		return errors.New("android release checksum is invalid")
	}
	if record.Filename == "" || filepath.Base(record.Filename) != record.Filename || !strings.HasSuffix(record.Filename, ".apk") {
		return errors.New("android release filename is invalid")
	}
	if record.PublishedAt.IsZero() || !utf8.ValidString(record.Message) || len([]rune(record.Message)) > MaxMessageRunes {
		return errors.New("android release details are invalid")
	}
	return nil
}

func inspectAPK(path string) (apkMetadata, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return apkMetadata{}, ErrInvalidAPK
	}
	defer archive.Close()
	var hasManifest, hasDex bool
	var metadataFile *zip.File
	for _, file := range archive.File {
		switch file.Name {
		case "AndroidManifest.xml":
			hasManifest = true
		case "classes.dex":
			hasDex = true
		case metadataPath:
			metadataFile = file
		}
	}
	if !hasManifest || !hasDex || metadataFile == nil || metadataFile.UncompressedSize64 > 16<<10 {
		return apkMetadata{}, ErrInvalidAPK
	}
	reader, err := metadataFile.Open()
	if err != nil {
		return apkMetadata{}, ErrInvalidAPK
	}
	defer reader.Close()
	var metadata apkMetadata
	decoder := json.NewDecoder(io.LimitReader(reader, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return apkMetadata{}, ErrInvalidAPK
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apkMetadata{}, ErrInvalidAPK
	}
	if metadata.ApplicationID != ApplicationID || metadata.VersionCode <= 0 || !versionNamePattern.MatchString(metadata.VersionName) {
		return apkMetadata{}, ErrInvalidAPK
	}
	return metadata, nil
}

func writeReleaseRecordAtomic(directory string, release *Release) error {
	record := releaseRecord{Release: *release, Filename: release.Filename}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Android release metadata: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".marvo-android-release-*")
	if err != nil {
		return fmt.Errorf("create Android release metadata: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, filepath.Join(directory, releaseFile)); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("publish Android release metadata: %w", err)
	}
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a regular directory")
	}
	return os.Chmod(path, 0700)
}

func openRegularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	return os.Open(path)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
