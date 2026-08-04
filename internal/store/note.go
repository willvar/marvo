package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrNoteNotFound      = errors.New("note not found")
	ErrNoteAlreadyExists = errors.New("note already exists")
	ErrRevisionConflict  = errors.New("note revision conflict")
	ErrInstanceChanged   = errors.New("note instance changed")
)

const (
	ConflictRevision = "content_revision_conflict"
	ConflictMeta     = "meta_revision_conflict"
	ConflictInstance = "note_instance_changed"
)

// Meta contains user-visible note metadata. Title is deliberately not stored
// here: it is the current directory name and may be changed by moving the note.
type Meta struct {
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type NoteInfo struct {
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NoteSnapshot struct {
	Note            NoteInfo `json:"note"`
	Content         string   `json:"content"`
	ContentRevision string   `json:"content_revision"`
	MetaRevision    string   `json:"meta_revision"`
	InstanceToken   string   `json:"instance_token"`
}

// ConflictError carries enough current state for the client to decide whether
// a three-way content merge is safe. Instance conflicts must never be merged
// automatically, even when the two documents happen to have equal contents.
type ConflictError struct {
	Kind      string
	Title     string
	MovedTo   string
	Current   *NoteSnapshot
	Requested string
}

func (e *ConflictError) Error() string {
	if e == nil {
		return "note conflict"
	}
	if e.MovedTo != "" {
		return fmt.Sprintf("%s: note moved to %s", e.Kind, e.MovedTo)
	}
	return e.Kind
}

func (e *ConflictError) Unwrap() error {
	if e != nil && e.Kind == ConflictInstance {
		return ErrInstanceChanged
	}
	return ErrRevisionConflict
}

type MetaUpdate struct {
	Tags *[]string
}

type noteInstance struct {
	token string
	info  os.FileInfo
}

type NoteStore struct {
	dataDir      string
	mu           sync.RWMutex
	instances    map[string]*noteInstance
	tokenToTitle map[string]string
}

func NewNoteStore(dataDir string) *NoteStore {
	return &NoteStore{
		dataDir:      filepath.Clean(dataDir),
		instances:    make(map[string]*noteInstance),
		tokenToTitle: make(map[string]string),
	}
}

func (s *NoteStore) DataDir() string { return s.dataDir }

func ValidateTitle(title string) error {
	if title == "" {
		return errors.New("title cannot be empty")
	}
	if !utf8.ValidString(title) {
		return errors.New("title must be valid UTF-8")
	}
	if title != strings.TrimSpace(title) {
		return errors.New("title cannot start or end with whitespace")
	}
	if len([]rune(title)) > 200 {
		return errors.New("title too long (max 200 characters)")
	}
	if title == "." || title == ".." || strings.HasPrefix(title, ".") {
		return errors.New("title cannot be hidden or relative")
	}
	if title != filepath.Base(title) || strings.ContainsAny(title, `/\\`) || filepath.IsAbs(title) {
		return errors.New("title must be a single path segment")
	}
	if strings.ContainsAny(title, `:*?"<>|`) {
		return errors.New("title contains unsupported path characters")
	}
	for _, r := range title {
		if r == 0 || unicode.IsControl(r) {
			return errors.New("title contains control characters")
		}
	}
	return nil
}

func ContentRevision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *NoteStore) noteDir(title string) (string, error) {
	if err := ValidateTitle(title); err != nil {
		return "", err
	}
	return filepath.Join(s.dataDir, title), nil
}

func regularDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoteNotFound
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, ErrNoteNotFound
	}
	return info, nil
}

func (s *NoteStore) List() ([]NoteInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read data directory: %w", err)
	}

	notes := make([]NoteInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, _, err := s.getNoteInfoUnlocked(entry.Name())
		if err != nil {
			slog.Warn("skipping invalid note", "title", entry.Name(), "error", err)
			continue
		}
		notes = append(notes, info)
	}
	return notes, nil
}

func (s *NoteStore) Snapshot(title string) (*NoteSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotUnlocked(title)
}

// Get remains as a small compatibility helper for search and watcher callers.
func (s *NoteStore) Get(title string) (*NoteInfo, string, error) {
	snapshot, err := s.Snapshot(title)
	if err != nil {
		return nil, "", err
	}
	info := snapshot.Note
	return &info, snapshot.Content, nil
}

func (s *NoteStore) snapshotUnlocked(title string) (*NoteSnapshot, error) {
	info, metaRaw, err := s.getNoteInfoUnlocked(title)
	if err != nil {
		return nil, err
	}
	dir, _ := s.noteDir(title)
	content, err := readRegularFile(filepath.Join(dir, "index.md"))
	if err != nil {
		return nil, fmt.Errorf("read note content: %w", err)
	}
	instance, err := s.instanceUnlocked(title)
	if err != nil {
		return nil, err
	}
	return &NoteSnapshot{
		Note:            info,
		Content:         string(content),
		ContentRevision: ContentRevision(content),
		MetaRevision:    ContentRevision(metaRaw),
		InstanceToken:   instance.token,
	}, nil
}

func (s *NoteStore) CreateNote(title, content string, tags []string) (*NoteSnapshot, error) {
	if err := ValidateTitle(title); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dir, _ := s.noteDir(title)
	if _, err := os.Lstat(dir); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoteAlreadyExists, title)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoteAlreadyExists, title)
		}
		return nil, fmt.Errorf("create note directory: %w", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(filepath.Join(dir, "index.md"))
			_ = os.Remove(filepath.Join(dir, "meta.json"))
			_ = os.Remove(dir)
		}
	}()

	if err := atomicWriteFile(filepath.Join(dir, "index.md"), []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write note content: %w", err)
	}
	meta := Meta{Tags: normalizeTags(tags), CreatedAt: time.Now().UTC()}
	if err := s.writeMetaUnlocked(dir, meta); err != nil {
		return nil, fmt.Errorf("write note metadata: %w", err)
	}
	created = false
	s.invalidateInstanceUnlocked(title)
	snapshot, err := s.snapshotUnlocked(title)
	if err == nil {
		slog.Info("note created", "title", title)
	}
	return snapshot, err
}

func (s *NoteStore) UpdateContentCAS(title, instanceToken, baseRevision, content string) (*NoteSnapshot, error) {
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
	if baseRevision == "" || current.ContentRevision != baseRevision {
		return nil, &ConflictError{Kind: ConflictRevision, Title: title, Current: current, Requested: baseRevision}
	}

	dir, _ := s.noteDir(title)
	path := filepath.Join(dir, "index.md")
	// Re-read immediately before the atomic rename. This catches filesystem
	// writers that changed the note while the request body was being processed.
	latest, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}
	if revision := ContentRevision(latest); revision != baseRevision {
		latestSnapshot, snapErr := s.snapshotUnlocked(title)
		if snapErr != nil {
			return nil, snapErr
		}
		return nil, &ConflictError{Kind: ConflictRevision, Title: title, Current: latestSnapshot, Requested: baseRevision}
	}
	if err := atomicWriteFile(path, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("write note content: %w", err)
	}
	return s.snapshotUnlocked(title)
}

func (s *NoteStore) UpdateMetaCAS(title, instanceToken, baseRevision string, update MetaUpdate) (*NoteSnapshot, error) {
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
	if baseRevision == "" || current.MetaRevision != baseRevision {
		return nil, &ConflictError{Kind: ConflictMeta, Title: title, Current: current, Requested: baseRevision}
	}
	dir, _ := s.noteDir(title)
	meta, latestRaw, err := s.readMetaUnlocked(dir, title)
	if err != nil {
		return nil, err
	}
	if revision := ContentRevision(latestRaw); revision != baseRevision {
		latestSnapshot, snapErr := s.snapshotUnlocked(title)
		if snapErr != nil {
			return nil, snapErr
		}
		return nil, &ConflictError{Kind: ConflictMeta, Title: title, Current: latestSnapshot, Requested: baseRevision}
	}
	if update.Tags != nil {
		meta.Tags = normalizeTags(*update.Tags)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = current.Note.CreatedAt.UTC()
	}
	if err := s.writeMetaUnlocked(dir, meta); err != nil {
		return nil, fmt.Errorf("write note metadata: %w", err)
	}
	return s.snapshotUnlocked(title)
}

func (s *NoteStore) RenameCAS(oldTitle, newTitle, instanceToken string) (*NoteSnapshot, error) {
	if err := ValidateTitle(newTitle); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.snapshotUnlocked(oldTitle)
	if err != nil {
		if errors.Is(err, ErrNoteNotFound) && instanceToken != "" {
			return nil, s.instanceConflictUnlocked(oldTitle, instanceToken, nil)
		}
		return nil, err
	}
	if current.InstanceToken != instanceToken || instanceToken == "" {
		return nil, s.instanceConflictUnlocked(oldTitle, instanceToken, current)
	}
	if oldTitle == newTitle {
		return current, nil
	}
	oldDir, _ := s.noteDir(oldTitle)
	newDir, _ := s.noteDir(newTitle)
	if _, err := os.Lstat(newDir); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoteAlreadyExists, newTitle)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, fmt.Errorf("rename note: %w", err)
	}
	instance := s.instances[oldTitle]
	delete(s.instances, oldTitle)
	if instance != nil {
		if info, statErr := os.Lstat(newDir); statErr == nil {
			instance.info = info
		}
		s.instances[newTitle] = instance
		s.tokenToTitle[instance.token] = newTitle
	}
	slog.Info("note renamed", "old_title", oldTitle, "new_title", newTitle)
	return s.snapshotUnlocked(newTitle)
}

func (s *NoteStore) ResolveInstance(instanceToken string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	title, ok := s.tokenToTitle[instanceToken]
	return title, ok
}

func (s *NoteStore) invalidateInstanceUnlocked(title string) {
	if old := s.instances[title]; old != nil {
		delete(s.tokenToTitle, old.token)
	}
	delete(s.instances, title)
}

func (s *NoteStore) instanceUnlocked(title string) (*noteInstance, error) {
	dir, err := s.noteDir(title)
	if err != nil {
		return nil, err
	}
	info, err := regularDirectory(dir)
	if err != nil {
		return nil, err
	}
	if current := s.instances[title]; current != nil && current.info != nil && os.SameFile(current.info, info) {
		current.info = info
		return current, nil
	}
	s.invalidateInstanceUnlocked(title)
	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate note instance token: %w", err)
	}
	instance := &noteInstance{token: token, info: info}
	s.instances[title] = instance
	s.tokenToTitle[token] = title
	return instance, nil
}

func (s *NoteStore) instanceConflictUnlocked(title, requested string, current *NoteSnapshot) error {
	movedTo := ""
	if requested != "" {
		movedTo = s.tokenToTitle[requested]
	}
	return &ConflictError{
		Kind:      ConflictInstance,
		Title:     title,
		MovedTo:   movedTo,
		Current:   current,
		Requested: requested,
	}
}

func (s *NoteStore) getNoteInfoUnlocked(title string) (NoteInfo, []byte, error) {
	dir, err := s.noteDir(title)
	if err != nil {
		return NoteInfo{}, nil, err
	}
	dirStat, err := regularDirectory(dir)
	if err != nil {
		return NoteInfo{}, nil, err
	}
	meta, metaRaw, err := s.readMetaUnlocked(dir, title)
	if err != nil {
		slog.Warn("failed to read note metadata, using safe defaults", "title", title, "error", err)
		meta = Meta{Tags: []string{}, CreatedAt: dirStat.ModTime().UTC()}
		metaRaw = nil
	}
	mdStat, err := regularFileInfo(filepath.Join(dir, "index.md"))
	if err != nil {
		return NoteInfo{}, nil, fmt.Errorf("invalid note content: %w", err)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = dirStat.ModTime().UTC()
	}
	return NoteInfo{
		Title:     title,
		Tags:      normalizeTags(meta.Tags),
		CreatedAt: meta.CreatedAt,
		UpdatedAt: mdStat.ModTime().UTC(),
	}, metaRaw, nil
}

func (s *NoteStore) readMetaUnlocked(dir, title string) (Meta, []byte, error) {
	path := filepath.Join(dir, "meta.json")
	data, err := readRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Meta{Tags: []string{}}, nil, nil
	}
	if err != nil {
		return Meta{}, nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, data, err
	}
	meta.Tags = normalizeTags(meta.Tags)
	return meta, data, nil
}

func (s *NoteStore) writeMetaUnlocked(dir string, meta Meta) error {
	meta.Tags = normalizeTags(meta.Tags)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(filepath.Join(dir, "meta.json"), data, 0600)
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	return os.ReadFile(path)
}

func regularFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	return info, nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if _, err := regularDirectory(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".marvo-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(mode); err != nil {
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
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

func (s *NoteStore) AttachmentPath(title, filename string) (string, error) {
	if !ValidAssetFilename(filename) {
		return "", errors.New("invalid asset filename")
	}
	dir, err := s.noteDir(title)
	if err != nil {
		return "", err
	}
	if _, err := regularDirectory(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, "assets", filename), nil
}

// AssetsDirCAS resolves the current, regular assets directory only when the
// caller still refers to the same in-memory note instance. Long-running media
// work can call ResolveInstance first so a product-level title rename does not
// strand the job.
func (s *NoteStore) AssetsDirCAS(title, instanceToken string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.snapshotUnlocked(title)
	if err != nil {
		return "", err
	}
	if instanceToken == "" || current.InstanceToken != instanceToken {
		return "", s.instanceConflictUnlocked(title, instanceToken, current)
	}
	dir, _ := s.noteDir(title)
	assetsDir := filepath.Join(dir, "assets")
	if info, err := os.Lstat(assetsDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("assets path is not a regular directory")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(assetsDir, 0700); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	return assetsDir, nil
}

func ValidAssetFilename(filename string) bool {
	return filename != "" &&
		filename == filepath.Base(filename) &&
		!strings.Contains(filename, "..") &&
		!strings.ContainsAny(filename, `/\\`) &&
		!filepath.IsAbs(filename) &&
		!strings.HasPrefix(filename, ".")
}

func (s *NoteStore) OpenAttachment(title, filename string) (*os.File, os.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, err := s.AttachmentPath(title, filename)
	if err != nil {
		return nil, nil, err
	}
	info, err := regularFileInfo(path)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}
