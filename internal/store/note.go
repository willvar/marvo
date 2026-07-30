package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Meta struct {
	Tags []string `json:"tags"`
}

type NoteInfo struct {
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var invalidChars = "/\\:*?\"<>|"

func ValidateTitle(title string) error {
	if title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if strings.ContainsAny(title, invalidChars) {
		return fmt.Errorf("title contains invalid characters: %s", invalidChars)
	}
	if title == "." || title == ".." {
		return fmt.Errorf("title cannot be '.' or '..'")
	}
	if len(title) > 200 {
		return fmt.Errorf("title too long (max 200 characters)")
	}
	return nil
}

type NoteStore struct {
	dataDir string
	mu      sync.RWMutex
}

func NewNoteStore(dataDir string) *NoteStore {
	return &NoteStore{dataDir: dataDir}
}

func (s *NoteStore) noteDir(title string) string {
	return filepath.Join(s.dataDir, title)
}

func (s *NoteStore) Exists(title string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, err := os.Stat(s.noteDir(title))
	return err == nil && info.IsDir()
}

func (s *NoteStore) List() ([]NoteInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var notes []NoteInfo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := s.getNoteInfoUnlocked(entry.Name())
		if err != nil {
			slog.Warn("skipping invalid note", "title", entry.Name(), "error", err)
			continue
		}
		notes = append(notes, *info)
	}
	return notes, nil
}

func (s *NoteStore) Get(title string) (*NoteInfo, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := s.getNoteInfoUnlocked(title)
	if err != nil {
		return nil, "", err
	}

	content, err := os.ReadFile(filepath.Join(s.noteDir(title), "index.md"))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read note content: %w", err)
	}

	return info, string(content), nil
}

func (s *NoteStore) Create(title string, content string, tags []string) error {
	if err := ValidateTitle(title); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.noteDir(title)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("note already exists: %s", title)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create note directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(content), 0644); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("failed to write note content: %w", err)
	}

	meta := Meta{Tags: tags}
	if err := s.writeMeta(dir, &meta); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("failed to write meta: %w", err)
	}

	slog.Info("note created", "title", title)
	return nil
}

func (s *NoteStore) UpdateContent(title string, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.noteDir(title)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("note not found: %s", title)
	}

	return os.WriteFile(filepath.Join(dir, "index.md"), []byte(content), 0644)
}

func (s *NoteStore) UpdateMeta(title string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.noteDir(title)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("note not found: %s", title)
	}

	meta := Meta{Tags: tags}
	return s.writeMeta(dir, &meta)
}

func (s *NoteStore) Rename(oldTitle, newTitle string) error {
	if err := ValidateTitle(newTitle); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldDir := s.noteDir(oldTitle)
	newDir := s.noteDir(newTitle)

	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return fmt.Errorf("note not found: %s", oldTitle)
	}

	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("note already exists: %s", newTitle)
	}

	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("failed to rename note: %w", err)
	}

	slog.Info("note renamed", "old_title", oldTitle, "new_title", newTitle)
	return nil
}

func (s *NoteStore) Delete(title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.noteDir(title)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("note not found: %s", title)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}

	slog.Info("note deleted", "title", title)
	return nil
}

func (s *NoteStore) WriteAttachment(title string, filename string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.noteDir(title)
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return fmt.Errorf("failed to create assets directory: %w", err)
	}

	return os.WriteFile(filepath.Join(assetsDir, filename), data, 0644)
}

func (s *NoteStore) ReadAttachment(title string, filename string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return os.ReadFile(filepath.Join(s.noteDir(title), "assets", filename))
}

func (s *NoteStore) getNoteInfoUnlocked(title string) (*NoteInfo, error) {
	dir := s.noteDir(title)
	stat, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("note not found: %s", title)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", title)
	}

	meta, err := s.readMeta(dir)
	if err != nil {
		slog.Warn("failed to read meta, using defaults", "title", title, "error", err)
		meta = &Meta{}
	}

	mdStat, err := os.Stat(filepath.Join(dir, "index.md"))
	var updatedAt time.Time
	if err != nil {
		updatedAt = stat.ModTime()
	} else {
		updatedAt = mdStat.ModTime()
	}

	tags := meta.Tags
	if tags == nil {
		tags = []string{}
	}

	return &NoteInfo{
		Title:     title,
		Tags:      tags,
		CreatedAt: stat.ModTime(),
		UpdatedAt: updatedAt,
	}, nil
}

func (s *NoteStore) readMeta(dir string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *NoteStore) writeMeta(dir string, meta *Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0644)
}
