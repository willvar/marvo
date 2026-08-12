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
	const userID = "68d91e1697014b8981e8"
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
	const userID = "4f99090663714ff6b785"
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

func TestUserUsageCountsRegularFilesWithoutFollowingSymlinks(t *testing.T) {
	root := t.TempDir()
	layout, err := OpenLayout(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	const userID = "20a903a6f7864975ac2e"
	paths, err := layout.EnsureUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Workspace, "note.md"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.App, "settings.json"), []byte("1234567"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("must not count"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(paths.Agent, "outside-link")); err != nil {
		t.Fatal(err)
	}

	used, err := layout.UserUsage(userID)
	if err != nil {
		t.Fatal(err)
	}
	if used != 12 {
		t.Fatalf("UserUsage() = %d, want 12", used)
	}
}
