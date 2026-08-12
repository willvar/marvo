package userspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutCreatesPrivateUserBoundaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	layout, err := OpenLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	const userID = "68d91e16-9701-4b89-81e8-8510d807e132"
	paths, err := layout.EnsureUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "control"), filepath.Join(root, "users"), paths.Root, paths.App, paths.Workspace, paths.Agent} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("%s mode = %v", path, info.Mode())
		}
	}
	if got := layout.ControlDatabase(); got != filepath.Join(root, "control", "platform.sqlite") {
		t.Fatalf("control database = %q", got)
	}
}

func TestLayoutRejectsSymlinkedUserBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	layout, err := OpenLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	const userID = "4f990906-6371-4ff6-b785-fb880a9910b3"
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "users", userID)); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.EnsureUser(userID); err == nil {
		t.Fatal("symlinked user root was accepted")
	}
}

func TestLayoutRejectsInvalidUserID(t *testing.T) {
	layout, err := OpenLayout(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := layout.EnsureUser("../other-user"); err == nil {
		t.Fatal("path-like user id was accepted")
	}
}
