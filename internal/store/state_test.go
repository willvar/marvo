package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStateDB(t *testing.T) (*StateDB, string) {
	t.Helper()
	workspace := t.TempDir()
	state, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	return state, workspace
}

func TestStateDBUsesPrivateSQLiteAndUpgradesActivitySchema(t *testing.T) {
	workspace := t.TempDir()
	stateDirectory := filepath.Join(workspace, stateDirectoryName)
	if err := os.Mkdir(stateDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(stateDirectory, stateDatabaseName)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		CREATE TABLE state_meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO state_meta(key, value) VALUES('schema_version', '1');
		CREATE TABLE activities (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL,
			choices_json TEXT NOT NULL DEFAULT '[]', source_session_id TEXT NOT NULL,
			source_message_id TEXT NOT NULL, source_call_id TEXT NOT NULL, dedupe_key TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL, read_at INTEGER, responded_at INTEGER, archived_at INTEGER,
			response_text TEXT NOT NULL DEFAULT '', response_choices_json TEXT NOT NULL DEFAULT '[]',
			reply_session_id TEXT NOT NULL DEFAULT '', reply_reserved_at INTEGER
		);
	`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	state, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	info, err := os.Stat(state.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state database mode = %o, want 600", info.Mode().Perm())
	}
	var version int
	if err := state.sql.QueryRow(`SELECT CAST(value AS INTEGER) FROM state_meta WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != stateSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, stateSchemaVersion)
	}
	activities, err := NewActivityStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindChoice, Title: "选择", Content: "请选择", Choices: []string{"A", "B"}, Multiple: true,
		SourceSessionID: "session", SourceMessageID: "message",
	}); err != nil {
		t.Fatalf("publish using upgraded schema: %v", err)
	}
}

func TestStateDBMigratesLegacyStructuredStateAtomically(t *testing.T) {
	workspace := t.TempDir()
	approvedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	legacyDevices := deviceFile{
		Pending: map[string]*PendingRequest{
			"request": {ID: "request", LocalDeviceID: "pending-browser", DeviceName: "待审核设备", CreatedAt: approvedAt},
		},
		Approved: map[string]*approvedDeviceRecord{
			"browser": {
				ID: "device", LocalDeviceID: "browser", DeviceName: "已批准设备", Token: "device-token", ApprovedAt: approvedAt,
			},
		},
	}
	writeLegacyJSON(t, filepath.Join(workspace, agentSettingsFilename), AgentSettings{
		Model: &AgentModelSelection{ProviderID: "openai", ModelID: "gpt"}, Variant: "high", GlobalPrompt: "使用中文",
	})
	writeLegacyJSON(t, filepath.Join(workspace, brandFilename), BrandConfig{Name: "码窝"})
	writeLegacyJSON(t, filepath.Join(workspace, legacyMemoriesFilename), legacyMemoriesFile{Rules: []Memory{{
		ID: "2bd3d4d2-84df-4bb8-a9aa-774df442e950", Text: "统一使用智能体这一称呼。",
	}}})
	writeLegacyJSON(t, filepath.Join(workspace, ".devices.json"), legacyDevices)

	state, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	settings, _ := NewAgentSettingsStore(state)
	if got := settings.Get(); got.Model == nil || got.Model.ProviderID != "openai" || got.Variant != "high" || got.GlobalPrompt != "使用中文" {
		t.Fatalf("migrated settings = %#v", got)
	}
	brand, _ := NewBrandStore(state)
	if got := brand.Get().Name; got != "码窝" {
		t.Fatalf("migrated brand = %q", got)
	}
	memories, _ := NewMemoryStore(state)
	snapshot, err := memories.Get()
	if err != nil || len(snapshot.Memories) != 1 || snapshot.Memories[0].Text != "统一使用智能体这一称呼。" {
		t.Fatalf("migrated memories = %#v, error = %v", snapshot, err)
	}
	devices := NewDeviceStore(state, "secret")
	if len(devices.ListRequests()) != 1 || len(devices.ListDevices()) != 1 || !devices.VerifyToken("device-token", devices.SignToken("device-token")) {
		t.Fatal("legacy devices were not migrated")
	}
	for _, name := range []string{agentSettingsFilename, brandFilename, legacyMemoriesFilename, ".devices.json"} {
		if _, err := os.Lstat(filepath.Join(workspace, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy file %s was not removed: %v", name, err)
		}
	}
	recreated := filepath.Join(workspace, legacyMemoriesFilename)
	if err := os.WriteFile(recreated, []byte("user-created after migration\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if data, err := os.ReadFile(recreated); err != nil || string(data) != "user-created after migration\n" {
		t.Fatalf("post-migration file was altered: %q, %v", data, err)
	}
}

func TestStateDBPreservesInvalidLegacyState(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, legacyMemoriesFilename)
	if err := os.WriteFile(path, []byte(`{"rules":[],"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStateDB(workspace); err == nil {
		t.Fatal("invalid legacy state was accepted")
	}
	if data, err := os.ReadFile(path); err != nil || len(data) == 0 {
		t.Fatalf("invalid legacy source was not preserved: %q, %v", data, err)
	}
}

func writeLegacyJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
