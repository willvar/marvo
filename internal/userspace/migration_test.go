package userspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const migrationTestUserID = "b8c42977bc4e49779e04"

func TestLegacyMigrationCopiesUserDataWithoutLegacySystemFiles(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	layout, err := OpenLayout(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := filepath.Join(t.TempDir(), "data")
	legacyAgentHome := filepath.Join(t.TempDir(), "home")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "笔记", "index.md"), "正文")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "笔记", "meta.json"), `{"tags":[]}`)
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "笔记", "assets", "图片.png"), "image")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".trash", "entry", "index.md"), "deleted")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "theme.json"), `{"darkMode":true}`)
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".agent-settings.json"), `{"variant":""}`)
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".devices.json"), `{"requests":[],"devices":[]}`)
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".agent-personalization.json"), `{"rules":[]}`)
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".session-secret"), "must-not-copy")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "AGENTS.md"), "must-not-copy")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, ".search-index", "index"), "must-not-copy")
	mustWriteMigrationFile(t, filepath.Join(legacyAgentHome, ".local", "share", "opencode", "opencode.db"), "database")
	mustWriteMigrationFile(t, filepath.Join(legacyAgentHome, ".config", "opencode", "AGENTS.md"), "global prompt")
	mustWriteMigrationFile(t, filepath.Join(legacyAgentHome, ".cache", "opencode", "models.json"), "must-not-copy")

	sources := LegacySources{Workspace: legacyWorkspace, AgentHome: legacyAgentHome}
	status, err := layout.InspectLegacy(sources)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.NoteCount != 1 || !status.HasTrash || !status.HasSettings || !status.HasDevices || !status.HasAgentState {
		t.Fatalf("legacy status = %#v", status)
	}
	result, err := layout.MigrateLegacy(migrationTestUserID, sources)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoteCount != 1 || result.FilesCopied != 10 || result.BytesCopied == 0 {
		t.Fatalf("migration result = %#v", result)
	}
	paths, _ := layout.UserPaths(migrationTestUserID)
	assertMigrationFile(t, filepath.Join(paths.Workspace, "笔记", "index.md"), "正文")
	assertMigrationFile(t, filepath.Join(paths.App, ".agent-settings.json"), `{"variant":""}`)
	assertMigrationFile(t, filepath.Join(paths.Agent, "home", ".local", "share", "opencode", "opencode.db"), "database")
	assertMigrationMissing(t, filepath.Join(paths.Workspace, ".session-secret"))
	assertMigrationMissing(t, filepath.Join(paths.Workspace, "AGENTS.md"))
	assertMigrationMissing(t, filepath.Join(paths.Workspace, ".search-index"))
	assertMigrationMissing(t, filepath.Join(paths.Agent, "home", ".cache"))

	status, err = layout.InspectLegacy(sources)
	if err != nil || status.MigratedTo != migrationTestUserID {
		t.Fatalf("post-migration status = %#v, error = %v", status, err)
	}
	second, err := layout.MigrateLegacy(migrationTestUserID, sources)
	if err != nil || second.CompletedAt != result.CompletedAt {
		t.Fatalf("idempotent migration = %#v, error = %v", second, err)
	}
}

func TestLegacyMigrationNeverOverwritesDifferentTarget(t *testing.T) {
	layout, err := OpenLayout(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.EnsureUser(migrationTestUserID)
	if err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := filepath.Join(t.TempDir(), "data")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "同名", "index.md"), "legacy")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "同名", "meta.json"), `{"tags":[]}`)
	mustWriteMigrationFile(t, filepath.Join(paths.Workspace, "同名", "index.md"), "current")
	mustWriteMigrationFile(t, filepath.Join(paths.Workspace, "同名", "meta.json"), `{"tags":[]}`)

	_, err = layout.MigrateLegacy(migrationTestUserID, LegacySources{Workspace: legacyWorkspace, AgentHome: filepath.Join(t.TempDir(), "missing")})
	if !errors.Is(err, ErrMigrationConflict) {
		t.Fatalf("migration error = %v", err)
	}
	assertMigrationFile(t, filepath.Join(paths.Workspace, "同名", "index.md"), "current")
}

func TestLegacyMigrationRejectsSymbolicLinks(t *testing.T) {
	layout, err := OpenLayout(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := filepath.Join(t.TempDir(), "data")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "笔记", "index.md"), "content")
	mustWriteMigrationFile(t, filepath.Join(legacyWorkspace, "笔记", "meta.json"), `{"tags":[]}`)
	outside := filepath.Join(t.TempDir(), "outside")
	mustWriteMigrationFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(legacyWorkspace, "笔记", "assets-link")); err != nil {
		t.Fatal(err)
	}

	_, err = layout.MigrateLegacy(migrationTestUserID, LegacySources{Workspace: legacyWorkspace, AgentHome: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("migration accepted a symbolic link")
	}
	paths, _ := layout.UserPaths(migrationTestUserID)
	assertMigrationMissing(t, filepath.Join(paths.Workspace, "笔记", "index.md"))
}

func mustWriteMigrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertMigrationFile(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("file %q = %q, error = %v", path, raw, err)
	}
}

func assertMigrationMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be inspected: %v", path, err)
	}
}
