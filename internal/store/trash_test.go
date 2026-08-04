package store

import (
	"errors"
	"testing"
)

func TestTrashRestoreNeverOverwritesSameTitleReplacement(t *testing.T) {
	store := NewNoteStore(t.TempDir())
	original, err := store.CreateNote("daily", "old body", []string{"old"})
	if err != nil {
		t.Fatalf("CreateNote(original) error = %v", err)
	}
	entry, err := store.TrashCAS("daily", original.InstanceToken)
	if err != nil {
		t.Fatalf("TrashCAS() error = %v", err)
	}
	if _, err := store.CreateNote("daily", "new body", []string{"new"}); err != nil {
		t.Fatalf("CreateNote(replacement) error = %v", err)
	}

	if _, err := store.RestoreTrash(entry.ID, "daily"); !errors.Is(err, ErrNoteAlreadyExists) {
		t.Fatalf("RestoreTrash(collision) error = %#v, want ErrNoteAlreadyExists", err)
	}
	current, err := store.Snapshot("daily")
	if err != nil {
		t.Fatalf("Snapshot(replacement) error = %v", err)
	}
	if current.Content != "new body" || current.Note.Title != "daily" {
		t.Fatalf("replacement was changed: %#v", current)
	}
	entries, err := store.ListTrash()
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("ListTrash() = %#v, error = %v", entries, err)
	}

	restored, err := store.RestoreTrash(entry.ID, "Recovered daily")
	if err != nil {
		t.Fatalf("RestoreTrash(new title) error = %v", err)
	}
	if restored.Note.Title != "Recovered daily" || restored.Content != "old body" {
		t.Fatalf("restored snapshot = %#v", restored)
	}
	entries, err = store.ListTrash()
	if err != nil || len(entries) != 0 {
		t.Fatalf("ListTrash() after restore = %#v, error = %v", entries, err)
	}
}

func TestTrashPermanentDeleteAndEmpty(t *testing.T) {
	store := NewNoteStore(t.TempDir())
	var ids []string
	for _, title := range []string{"one", "two"} {
		snapshot, err := store.CreateNote(title, title+" body", nil)
		if err != nil {
			t.Fatalf("CreateNote(%q) error = %v", title, err)
		}
		entry, err := store.TrashCAS(title, snapshot.InstanceToken)
		if err != nil {
			t.Fatalf("TrashCAS(%q) error = %v", title, err)
		}
		ids = append(ids, entry.ID)
	}
	if err := store.PermanentlyDeleteTrash(ids[0]); err != nil {
		t.Fatalf("PermanentlyDeleteTrash() error = %v", err)
	}
	removed, err := store.EmptyTrash()
	if err != nil || removed != 1 {
		t.Fatalf("EmptyTrash() = %d, %v; want 1, nil", removed, err)
	}
	entries, err := store.ListTrash()
	if err != nil || len(entries) != 0 {
		t.Fatalf("ListTrash() = %#v, %v", entries, err)
	}
}
