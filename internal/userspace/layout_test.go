package userspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"marvo/internal/agentcredentials"
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
	for _, path := range []string{
		root,
		filepath.Join(root, "control"),
		filepath.Join(root, "users"),
		paths.Root,
		paths.App,
		paths.Workspace,
		paths.Agent,
		paths.AgentHome,
		paths.OpenCodeData,
	} {
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
	if got := layout.AndroidReleaseDirectory(); got != filepath.Join(root, "control", "android") {
		t.Fatalf("Android release directory = %q", got)
	}
}

func TestEnsureUserMigratesAgentCredentialsBesideOpenCodeState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	layout, err := OpenLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	const userID = "9e2ef88f87ad4d07962c"
	paths, err := layout.EnsureUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(paths.App, agentcredentials.LegacyFileName)
	targetPath := filepath.Join(paths.OpenCodeData, agentcredentials.FileName)
	const encrypted = `{"version":1,"nonce":"legacy","ciphertext":"encrypted"}`
	if err := os.WriteFile(legacyPath, []byte(encrypted), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.EnsureUser(userID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy credential file still exists: %v", err)
	}
	raw, err := os.ReadFile(targetPath)
	if err != nil || string(raw) != encrypted {
		t.Fatalf("migrated credentials = %q, error = %v", raw, err)
	}
	if _, err := layout.EnsureUser(userID); err != nil {
		t.Fatalf("idempotent EnsureUser() error = %v", err)
	}
}

func TestEnsureUserRejectsConflictingAgentCredentialMigration(t *testing.T) {
	layout, err := OpenLayout(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	const userID = "77598e6c72714703a204"
	paths, err := layout.EnsureUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(paths.App, agentcredentials.LegacyFileName)
	targetPath := filepath.Join(paths.OpenCodeData, agentcredentials.FileName)
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.EnsureUser(userID); err == nil {
		t.Fatal("conflicting credential files were accepted")
	}
	for path, want := range map[string]string{legacyPath: "legacy", targetPath: "current"} {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Fatalf("file %q = %q, error = %v", path, raw, err)
		}
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
