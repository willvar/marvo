package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherEmitsSnapshotSignalForCompleteDirectoryMove(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	stagingDir := filepath.Join(root, "staging")
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stagingDir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewNoteStore(stagingDir).CreateNote("restored", "complete body", nil); err != nil {
		t.Fatalf("CreateNote(staging) error = %v", err)
	}
	noteChanges := make(chan string, 4)
	watcher, err := WatchNotes(dataDir, func(title string) { noteChanges <- title }, func() {}, func() {})
	if err != nil {
		t.Fatalf("WatchNotes() error = %v", err)
	}
	defer watcher.Close()

	if err := os.Rename(filepath.Join(stagingDir, "restored"), filepath.Join(dataDir, "restored")); err != nil {
		t.Fatalf("Rename(complete note) error = %v", err)
	}
	select {
	case title := <-noteChanges:
		if title != "restored" {
			t.Fatalf("note change title = %q", title)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("complete note directory move did not trigger a note snapshot signal")
	}
}
