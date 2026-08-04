package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const trashDirName = ".trash"
const trashManifestName = ".trash.json"

type trashManifest struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	DeletedAt time.Time `json:"deleted_at"`
}

type TrashEntry struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	DeletedAt time.Time `json:"deleted_at"`
}

func (s *NoteStore) trashRoot() string { return filepath.Join(s.dataDir, trashDirName) }

func validTrashID(id string) bool {
	if len(id) != 32 || id != filepath.Base(id) {
		return false
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func (s *NoteStore) TrashCAS(title, instanceToken string) (*TrashEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.snapshotUnlocked(title)
	if err != nil {
		if errors.Is(err, ErrNoteNotFound) && instanceToken != "" {
			return nil, s.instanceConflictUnlocked(title, instanceToken, nil)
		}
		return nil, err
	}
	if instanceToken == "" || current.InstanceToken != instanceToken {
		return nil, s.instanceConflictUnlocked(title, instanceToken, current)
	}
	if err := os.MkdirAll(s.trashRoot(), 0700); err != nil {
		return nil, fmt.Errorf("create trash directory: %w", err)
	}

	id, err := randomToken()
	if err != nil {
		return nil, err
	}
	entryDir := filepath.Join(s.trashRoot(), id)
	if _, err := os.Lstat(entryDir); err == nil {
		return nil, errors.New("trash id collision")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	noteDir, _ := s.noteDir(title)
	if err := os.Rename(noteDir, entryDir); err != nil {
		return nil, fmt.Errorf("move note to trash: %w", err)
	}
	manifest := trashManifest{
		ID:        id,
		Title:     current.Note.Title,
		DeletedAt: time.Now().UTC(),
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		raw = append(raw, '\n')
		err = atomicWriteFile(filepath.Join(entryDir, trashManifestName), raw, 0600)
	}
	if err != nil {
		if rollbackErr := os.Rename(entryDir, noteDir); rollbackErr != nil {
			return nil, fmt.Errorf("write trash metadata: %v; rollback failed: %w", err, rollbackErr)
		}
		return nil, fmt.Errorf("write trash metadata: %w", err)
	}
	s.invalidateInstanceUnlocked(title)
	return &TrashEntry{
		ID:        id,
		Title:     current.Note.Title,
		Tags:      current.Note.Tags,
		DeletedAt: manifest.DeletedAt,
	}, nil
}

func (s *NoteStore) ListTrash() ([]TrashEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.trashRoot())
	if errors.Is(err, os.ErrNotExist) {
		return []TrashEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]TrashEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validTrashID(entry.Name()) {
			continue
		}
		manifest, err := s.readTrashManifestUnlocked(entry.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(s.trashRoot(), entry.Name())
		meta, _, _ := s.readMetaUnlocked(dir, manifest.Title)
		result = append(result, TrashEntry{
			ID:        manifest.ID,
			Title:     manifest.Title,
			Tags:      normalizeTags(meta.Tags),
			DeletedAt: manifest.DeletedAt,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeletedAt.After(result[j].DeletedAt) })
	return result, nil
}

func (s *NoteStore) RestoreTrash(id, newTitle string) (*NoteSnapshot, error) {
	if !validTrashID(id) {
		return nil, errors.New("invalid trash id")
	}
	if err := ValidateTitle(newTitle); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.readTrashManifestUnlocked(id)
	if err != nil {
		return nil, err
	}
	entryDir := filepath.Join(s.trashRoot(), id)
	destination, _ := s.noteDir(newTitle)
	if _, err := os.Lstat(destination); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoteAlreadyExists, newTitle)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := os.Rename(entryDir, destination); err != nil {
		return nil, fmt.Errorf("restore note: %w", err)
	}
	if err := os.Remove(filepath.Join(destination, trashManifestName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		// The note is already restored. Do not tell the client the operation
		// failed and invite a duplicate retry; surface cleanup only to operators.
		slog.Warn("restored note but failed to remove trash metadata", "title", newTitle, "error", err)
	}
	s.invalidateInstanceUnlocked(newTitle)
	snapshot, err := s.snapshotUnlocked(newTitle)
	if err == nil {
		_ = manifest
	}
	return snapshot, err
}

func (s *NoteStore) PermanentlyDeleteTrash(id string) error {
	if !validTrashID(id) {
		return errors.New("invalid trash id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entryDir := filepath.Join(s.trashRoot(), id)
	if _, err := regularDirectory(entryDir); err != nil {
		return err
	}
	if _, err := s.readTrashManifestUnlocked(id); err != nil {
		return err
	}
	return os.RemoveAll(entryDir)
}

func (s *NoteStore) EmptyTrash() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.trashRoot())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !validTrashID(entry.Name()) {
			continue
		}
		if _, err := s.readTrashManifestUnlocked(entry.Name()); err != nil {
			continue
		}
		path := filepath.Join(s.trashRoot(), entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (s *NoteStore) readTrashManifestUnlocked(id string) (trashManifest, error) {
	if !validTrashID(id) {
		return trashManifest{}, errors.New("invalid trash id")
	}
	dir := filepath.Join(s.trashRoot(), id)
	if _, err := regularDirectory(dir); err != nil {
		return trashManifest{}, err
	}
	raw, err := readRegularFile(filepath.Join(dir, trashManifestName))
	if err != nil {
		return trashManifest{}, err
	}
	var manifest trashManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return trashManifest{}, err
	}
	if manifest.ID != id || !validTrashID(manifest.ID) || ValidateTitle(manifest.Title) != nil {
		return trashManifest{}, errors.New("invalid trash metadata")
	}
	return manifest, nil
}
