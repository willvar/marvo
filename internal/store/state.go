package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	stateDirectoryName = ".marvo"
	stateDatabaseName  = "state.sqlite"
	stateSchemaVersion = 3
)

// StateDB owns the structured state for one user space. Notes, media and
// OpenCode state deliberately remain outside this database.
type StateDB struct {
	sql  *sql.DB
	path string
}

func OpenStateDB(workspace string) (*StateDB, error) {
	workspace = filepath.Clean(workspace)
	if !filepath.IsAbs(workspace) {
		return nil, errors.New("state workspace path must be absolute")
	}
	if info, err := os.Lstat(workspace); err != nil {
		return nil, fmt.Errorf("inspect state workspace: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("state workspace is not a regular directory")
	}

	directory := filepath.Join(workspace, stateDirectoryName)
	if err := ensurePrivateStateDirectory(directory); err != nil {
		return nil, fmt.Errorf("initialize user state directory: %w", err)
	}
	path := filepath.Join(directory, stateDatabaseName)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("user state database is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect user state database: %w", err)
	}

	databaseURL := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := databaseURL.Query()
	query.Set("_txlock", "immediate")
	databaseURL.RawQuery = query.Encode()
	sqlDB, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("open user state database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	db := &StateDB{sql: sqlDB, path: path}
	if err := db.initialize(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("protect user state database: %w", err)
	}
	if err := db.migrateLegacyJSON(context.Background(), workspace); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (d *StateDB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

func (d *StateDB) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *StateDB) initialize(ctx context.Context) error {
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := d.sql.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure user state database: %w", err)
		}
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin user state schema migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS state_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS space_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			brand_name TEXT NOT NULL DEFAULT 'Marvo',
			agent_provider_id TEXT,
			agent_model_id TEXT,
			agent_variant TEXT NOT NULL DEFAULT '',
			agent_global_prompt TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			text TEXT NOT NULL UNIQUE,
			position INTEGER NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS device_requests (
			id TEXT PRIMARY KEY,
			local_device_id TEXT NOT NULL UNIQUE,
			device_name TEXT NOT NULL,
			info_json TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS device_requests_created_idx
			ON device_requests(created_at, id);
		CREATE TABLE IF NOT EXISTS devices (
			local_device_id TEXT PRIMARY KEY,
			id TEXT NOT NULL UNIQUE,
			device_name TEXT NOT NULL,
			device_name_key TEXT NOT NULL UNIQUE,
			info_json TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			approved_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS devices_approved_idx
			ON devices(approved_at DESC, id);
		CREATE TABLE IF NOT EXISTS activities (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK (kind IN ('notice', 'choice')),
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			choices_json TEXT NOT NULL DEFAULT '[]',
			multiple INTEGER NOT NULL DEFAULT 0 CHECK (multiple IN (0, 1)),
			source_session_id TEXT NOT NULL,
			source_message_id TEXT NOT NULL,
			source_call_id TEXT NOT NULL DEFAULT '',
			dedupe_key TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			read_at INTEGER,
			responded_at INTEGER,
			response_text TEXT NOT NULL DEFAULT '',
			response_choices_json TEXT NOT NULL DEFAULT '[]',
			reply_session_id TEXT NOT NULL DEFAULT '',
			reply_reserved_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS activities_feed_idx
			ON activities(created_at DESC, id DESC);
		CREATE INDEX IF NOT EXISTS activities_unread_idx
			ON activities(read_at);
		CREATE INDEX IF NOT EXISTS activities_pending_idx
			ON activities(kind, responded_at);
		CREATE TABLE IF NOT EXISTS connectors (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
			config_nonce TEXT NOT NULL,
			config_ciphertext TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS connectors_provider_idx
			ON connectors(provider_id, created_at, id);
		CREATE TABLE IF NOT EXISTS activity_deliveries (
			id TEXT PRIMARY KEY,
			activity_id TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
			connector_id TEXT NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL,
			lease_until INTEGER,
			last_error TEXT NOT NULL DEFAULT '',
			sent_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(activity_id, connector_id)
		);
		CREATE INDEX IF NOT EXISTS activity_deliveries_due_idx
			ON activity_deliveries(status, next_attempt_at, lease_until);
		CREATE INDEX IF NOT EXISTS activity_deliveries_activity_idx
			ON activity_deliveries(activity_id, created_at, id);
	`); err != nil {
		return fmt.Errorf("create user state schema: %w", err)
	}

	var existingVersion int
	err = tx.QueryRowContext(ctx, "SELECT CAST(value AS INTEGER) FROM state_meta WHERE key = 'schema_version'").Scan(&existingVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read user state schema version: %w", err)
	}
	if err == nil && existingVersion > stateSchemaVersion {
		return fmt.Errorf("user state schema %d is newer than supported schema %d", existingVersion, stateSchemaVersion)
	}
	if err := ensureActivityMultipleColumn(ctx, tx); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO space_settings(id, updated_at) VALUES(1, ?)
		ON CONFLICT(id) DO NOTHING
	`, now); err != nil {
		return fmt.Errorf("initialize user settings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, strconv.Itoa(stateSchemaVersion)); err != nil {
		return fmt.Errorf("record user state schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user state schema migration: %w", err)
	}
	if _, err := d.sql.ExecContext(ctx, `
		UPDATE activities
		SET response_text = '', response_choices_json = '[]', reply_session_id = '', reply_reserved_at = NULL
		WHERE responded_at IS NULL AND reply_reserved_at IS NOT NULL AND reply_reserved_at < ?
	`, time.Now().UTC().Add(-10*time.Minute).UnixMilli()); err != nil {
		return fmt.Errorf("recover interrupted Activity replies: %w", err)
	}
	return nil
}

func ensureActivityMultipleColumn(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(activities)`)
	if err != nil {
		return fmt.Errorf("inspect Activity schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("inspect Activity schema column: %w", err)
		}
		if name == "multiple" {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("inspect Activity schema: %w", err)
	}
	if found {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE activities ADD COLUMN multiple INTEGER NOT NULL DEFAULT 0 CHECK (multiple IN (0, 1))`); err != nil {
		return fmt.Errorf("migrate Activity selection mode: %w", err)
	}
	return nil
}

type legacyState struct {
	settings *AgentSettings
	brand    *BrandConfig
	memories []Memory
	devices  *deviceFile
}

func (d *StateDB) migrateLegacyJSON(ctx context.Context, workspace string) error {
	var imported string
	err := d.sql.QueryRowContext(ctx, "SELECT value FROM state_meta WHERE key = 'legacy_json_imported'").Scan(&imported)
	if err == nil && imported == "1" {
		return d.finishLegacyJSONMigration(ctx, workspace)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect legacy user state migration: %w", err)
	}

	legacy, err := readLegacyState(workspace)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy user state migration: %w", err)
	}
	defer tx.Rollback()
	var concurrentlyImported string
	err = tx.QueryRowContext(ctx, "SELECT value FROM state_meta WHERE key = 'legacy_json_imported'").Scan(&concurrentlyImported)
	if err == nil && concurrentlyImported == "1" {
		_ = tx.Rollback()
		return d.finishLegacyJSONMigration(ctx, workspace)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("recheck legacy user state migration: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	if legacy.settings != nil {
		settings := legacy.settings
		var provider any
		var model any
		if settings.Model != nil {
			provider = settings.Model.ProviderID
			model = settings.Model.ModelID
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE space_settings
			SET agent_provider_id = ?, agent_model_id = ?, agent_variant = ?, agent_global_prompt = ?, updated_at = ?
			WHERE id = 1
		`, provider, model, settings.Variant, settings.GlobalPrompt, now); err != nil {
			return fmt.Errorf("migrate Agent settings: %w", err)
		}
	}
	if legacy.brand != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE space_settings SET brand_name = ?, updated_at = ? WHERE id = 1`, legacy.brand.Name, now); err != nil {
			return fmt.Errorf("migrate brand settings: %w", err)
		}
	}
	for position, memory := range legacy.memories {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memories(id, text, position, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?)
		`, memory.ID, memory.Text, position, now, now); err != nil {
			return fmt.Errorf("migrate memory: %w", err)
		}
	}
	if legacy.devices != nil {
		if err := migrateLegacyDevices(ctx, tx, legacy.devices); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO state_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return fmt.Errorf("record legacy user state migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy user state migration: %w", err)
	}
	return d.finishLegacyJSONMigration(ctx, workspace)
}

func (d *StateDB) finishLegacyJSONMigration(ctx context.Context, workspace string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy user state cleanup: %w", err)
	}
	defer tx.Rollback()
	var cleaned string
	err = tx.QueryRowContext(ctx, "SELECT value FROM state_meta WHERE key = 'legacy_json_cleaned'").Scan(&cleaned)
	if err == nil && cleaned == "1" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect legacy user state cleanup: %w", err)
	}
	if err := removeLegacyStateFiles(workspace); err != nil {
		return fmt.Errorf("remove migrated user state files: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO state_meta(key, value) VALUES('legacy_json_cleaned', '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`); err != nil {
		return fmt.Errorf("record legacy user state cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy user state cleanup: %w", err)
	}
	return nil
}

func readLegacyState(workspace string) (legacyState, error) {
	var result legacyState
	settingsPath := filepath.Join(workspace, agentSettingsFilename)
	if found, err := readStrictLegacyJSON(settingsPath, maxAgentSettingsBytes, &result.settings); err != nil {
		return result, fmt.Errorf("read legacy Agent settings: %w", err)
	} else if found {
		if result.settings == nil {
			return result, errors.New("legacy Agent settings cannot be null")
		}
		result.settings.Variant = stringsTrim(result.settings.Variant)
		if err := validateAgentSettings(*result.settings); err != nil {
			return result, err
		}
	}

	brandPath := filepath.Join(workspace, brandFilename)
	if found, err := readStrictLegacyJSON(brandPath, maxBrandFileBytes, &result.brand); err != nil {
		return result, fmt.Errorf("read legacy brand settings: %w", err)
	} else if found {
		if result.brand == nil {
			return result, errors.New("legacy brand settings cannot be null")
		}
		result.brand.Name = stringsTrim(result.brand.Name)
		if err := ValidateBrandName(result.brand.Name); err != nil {
			return result, err
		}
	}

	memoriesPath := filepath.Join(workspace, legacyMemoriesFilename)
	var legacyMemories legacyMemoriesFile
	if found, err := readStrictLegacyJSON(memoriesPath, MaxMemoriesBytes, &legacyMemories); err != nil {
		return result, fmt.Errorf("read legacy memories: %w", err)
	} else if found {
		memories, err := normalizeMemories(legacyMemories.Rules, false)
		if err != nil {
			return result, err
		}
		result.memories = memories
	}

	devicesPath := filepath.Join(workspace, ".devices.json")
	var devices deviceFile
	if found, err := readStrictLegacyJSON(devicesPath, 8<<20, &devices); err != nil {
		return result, fmt.Errorf("read legacy devices: %w", err)
	} else if found {
		if devices.Pending == nil {
			devices.Pending = make(map[string]*PendingRequest)
		}
		if devices.Approved == nil {
			devices.Approved = make(map[string]*approvedDeviceRecord)
		}
		result.devices = &devices
	}
	return result, nil
}

func readStrictLegacyJSON(path string, maximum int64, target any) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("legacy state path is not a regular file")
	}
	if info.Size() > maximum {
		return false, errors.New("legacy state file is too large")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return false, errors.New("legacy state file must contain one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return false, err
	}
	return true, nil
}

func migrateLegacyDevices(ctx context.Context, tx *sql.Tx, legacy *deviceFile) error {
	for id, request := range legacy.Pending {
		if request == nil || request.ID != id || request.LocalDeviceID == "" || request.ID == "" || !validDeviceName(normalizeDeviceName(request.DeviceName)) {
			return errors.New("legacy device request is invalid")
		}
		info, err := json.Marshal(request.DeviceInfo)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO device_requests(id, local_device_id, device_name, info_json, created_at)
			VALUES(?, ?, ?, ?, ?)
		`, request.ID, request.LocalDeviceID, normalizeDeviceName(request.DeviceName), string(info), request.CreatedAt.UTC().UnixMilli()); err != nil {
			return fmt.Errorf("migrate device request: %w", err)
		}
	}

	devices := make([]*approvedDeviceRecord, 0, len(legacy.Approved))
	for localID, device := range legacy.Approved {
		if device == nil || device.LocalDeviceID != localID || device.ID == "" || device.Token == "" || device.LocalDeviceID == "" ||
			!validDeviceName(normalizeDeviceName(device.DeviceName)) {
			return errors.New("legacy approved device is invalid")
		}
		copy := *device
		devices = append(devices, &copy)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].ApprovedAt.Equal(devices[j].ApprovedAt) {
			return devices[i].ID < devices[j].ID
		}
		return devices[i].ApprovedAt.Before(devices[j].ApprovedAt)
	})
	claimed := make([]string, 0, len(devices))
	for _, device := range devices {
		device.DeviceName = nextUniqueDeviceName(device.DeviceName, func(candidate string) bool {
			for _, name := range claimed {
				if deviceNameKey(name) == deviceNameKey(candidate) {
					return true
				}
			}
			return false
		})
		claimed = append(claimed, device.DeviceName)
		info, err := json.Marshal(device.DeviceInfo)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO devices(local_device_id, id, device_name, device_name_key, info_json, token, approved_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)
		`, device.LocalDeviceID, device.ID, device.DeviceName, deviceNameKey(device.DeviceName), string(info), device.Token,
			device.ApprovedAt.UTC().UnixMilli()); err != nil {
			return fmt.Errorf("migrate approved device: %w", err)
		}
	}
	return nil
}

func removeLegacyStateFiles(workspace string) error {
	for _, name := range []string{agentSettingsFilename, brandFilename, legacyMemoriesFilename, ".devices.json"} {
		path := filepath.Join(workspace, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("legacy state path %s is not a regular file", name)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateStateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a regular directory")
	}
	return os.Chmod(path, 0700)
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
