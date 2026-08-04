package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListUsesDefaultsWhenMetaMissing(t *testing.T) {
	dataDir := t.TempDir()
	title := "new note"
	noteDir := filepath.Join(dataDir, title)

	if err := os.Mkdir(noteDir, 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(noteDir, "index.md"), []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	notes, err := NewNoteStore(dataDir).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("List() returned %d notes, want 1", len(notes))
	}
	if notes[0].Title != title {
		t.Fatalf("Title = %q, want %q", notes[0].Title, title)
	}
	if len(notes[0].Tags) != 0 {
		t.Fatalf("Tags = %v, want empty", notes[0].Tags)
	}
}

func TestContentCASRejectsStaleBrowserWrite(t *testing.T) {
	dataDir := t.TempDir()
	store := NewNoteStore(dataDir)
	base, err := store.CreateNote("cas-note", "base", nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}

	agentWrite, err := store.UpdateContentCAS("cas-note", base.InstanceToken, base.ContentRevision, "written by Agent")
	if err != nil {
		t.Fatalf("first UpdateContentCAS() error = %v", err)
	}
	_, err = store.UpdateContentCAS("cas-note", base.InstanceToken, base.ContentRevision, "stale browser document")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Kind != ConflictRevision {
		t.Fatalf("stale UpdateContentCAS() error = %#v, want content revision conflict", err)
	}
	if conflict.Current == nil || conflict.Current.Content != "written by Agent" || conflict.Current.ContentRevision != agentWrite.ContentRevision {
		t.Fatalf("conflict current snapshot = %#v", conflict.Current)
	}

	current, err := store.Snapshot("cas-note")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if current.Content != "written by Agent" {
		t.Fatalf("content = %q, stale browser overwrote Agent output", current.Content)
	}
}

func TestContentCASDetectsExternalFilesystemWrite(t *testing.T) {
	dataDir := t.TempDir()
	store := NewNoteStore(dataDir)
	base, err := store.CreateNote("external-edit", "base", nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}
	contentPath := filepath.Join(dataDir, "external-edit", "index.md")
	if err := os.WriteFile(contentPath, []byte("OpenCode edit"), 0600); err != nil {
		t.Fatalf("external WriteFile() error = %v", err)
	}

	_, err = store.UpdateContentCAS("external-edit", base.InstanceToken, base.ContentRevision, "browser overwrite")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Kind != ConflictRevision {
		t.Fatalf("UpdateContentCAS() error = %#v, want content revision conflict", err)
	}
	if conflict.Current == nil || conflict.Current.Content != "OpenCode edit" {
		t.Fatalf("conflict did not expose the current filesystem content: %#v", conflict.Current)
	}
	raw, readErr := os.ReadFile(contentPath)
	if readErr != nil || string(raw) != "OpenCode edit" {
		t.Fatalf("stored content = %q, error = %v", raw, readErr)
	}
}

func TestMetaCASDetectsExternalFilesystemWrite(t *testing.T) {
	dataDir := t.TempDir()
	store := NewNoteStore(dataDir)
	base, err := store.CreateNote("meta-edit", "body", nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}
	metaPath := filepath.Join(dataDir, "meta-edit", "meta.json")
	external := []byte("{\n  \"tags\": [\"agent\"]\n}\n")
	if err := os.WriteFile(metaPath, external, 0600); err != nil {
		t.Fatalf("external WriteFile() error = %v", err)
	}
	browserTags := []string{"browser"}

	_, err = store.UpdateMetaCAS("meta-edit", base.InstanceToken, base.MetaRevision, MetaUpdate{Tags: &browserTags})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Kind != ConflictMeta {
		t.Fatalf("UpdateMetaCAS() error = %#v, want metadata revision conflict", err)
	}
	if conflict.Current == nil || conflict.Current.Note.Title != "meta-edit" || len(conflict.Current.Note.Tags) != 1 || conflict.Current.Note.Tags[0] != "agent" {
		t.Fatalf("conflict current metadata = %#v", conflict.Current)
	}
}

func TestInstanceTokenRejectsSameTitleReplacement(t *testing.T) {
	dataDir := t.TempDir()
	originalStore := NewNoteStore(dataDir)
	original, err := originalStore.CreateNote("mutable", "old instance", nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}

	originalDir := filepath.Join(dataDir, "mutable")
	if err := os.Rename(originalDir, filepath.Join(dataDir, ".old-mutable")); err != nil {
		t.Fatalf("external rename error = %v", err)
	}
	replacement, err := NewNoteStore(dataDir).CreateNote("mutable", "new instance", nil)
	if err != nil {
		t.Fatalf("replacement CreateNote() error = %v", err)
	}

	_, err = originalStore.UpdateContentCAS("mutable", original.InstanceToken, original.ContentRevision, "stale draft")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Kind != ConflictInstance {
		t.Fatalf("UpdateContentCAS() error = %#v, want instance conflict", err)
	}
	if conflict.Current == nil || conflict.Current.InstanceToken == original.InstanceToken || conflict.Current.Content != replacement.Content {
		t.Fatalf("replacement snapshot = %#v", conflict.Current)
	}
	if errors.Is(err, ErrRevisionConflict) {
		t.Fatal("same-title replacement was incorrectly treated as mergeable revision conflict")
	}
}

func TestRenameKeepsIdentityAndReportsMovedTitle(t *testing.T) {
	store := NewNoteStore(t.TempDir())
	base, err := store.CreateNote("before", "body", nil)
	if err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}
	moved, err := store.RenameCAS("before", "after", base.InstanceToken)
	if err != nil {
		t.Fatalf("RenameCAS() error = %v", err)
	}
	if moved.InstanceToken != base.InstanceToken || moved.Note.Title != "after" {
		t.Fatalf("renamed snapshot = %#v", moved)
	}

	_, err = store.UpdateContentCAS("before", base.InstanceToken, base.ContentRevision, "stale URL")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Kind != ConflictInstance || conflict.MovedTo != "after" {
		t.Fatalf("old URL conflict = %#v", err)
	}
}

func TestCreatedNoteUsesPrivatePermissions(t *testing.T) {
	dataDir := t.TempDir()
	store := NewNoteStore(dataDir)
	if _, err := store.CreateNote("private", "body", nil); err != nil {
		t.Fatalf("CreateNote() error = %v", err)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(dataDir, "private"):              0700,
		filepath.Join(dataDir, "private", "index.md"):  0600,
		filepath.Join(dataDir, "private", "meta.json"): 0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("permissions for %s = %04o, want %04o", strings.TrimPrefix(path, dataDir), got, want)
		}
	}
	meta, err := os.ReadFile(filepath.Join(dataDir, "private", "meta.json"))
	if err != nil {
		t.Fatalf("ReadFile(meta.json) error = %v", err)
	}
	if strings.Contains(string(meta), `"title"`) {
		t.Fatalf("meta.json contains a second note name: %s", meta)
	}
}
