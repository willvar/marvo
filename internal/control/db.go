package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schemaVersion = 2

// DB owns the small control-plane database. User content and Agent state never
// live here; the database only stores identities and authentication metadata.
type DB struct {
	sql        *sql.DB
	totpKey    [32]byte
	totpIssuer string
}

func Open(path, sessionSecret string) (*DB, error) {
	if len(sessionSecret) < 32 {
		return nil, errors.New("control database requires a session secret of at least 32 characters")
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("control database path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create control directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("control database is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect control database: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open control database: %w", err)
	}
	// The control plane is tiny. A single connection gives transactions a clear
	// ordering and avoids connection-local SQLite pragma surprises.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := &DB{
		sql:        sqlDB,
		totpKey:    sha256.Sum256([]byte("marvo/control/totp/v1\x00" + sessionSecret)),
		totpIssuer: "Marvo",
	}
	if err := db.initialize(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("protect control database: %w", err)
	}
	return db, nil
}

func (d *DB) initialize(ctx context.Context) error {
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := d.sql.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure control database: %w", err)
		}
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin control schema migration: %w", err)
	}
	defer tx.Rollback()
	var metaExists int
	var existingVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'control_meta'
	`).Scan(&metaExists); err != nil {
		return fmt.Errorf("inspect control schema: %w", err)
	}
	if metaExists == 1 {
		err = tx.QueryRowContext(ctx, "SELECT CAST(value AS INTEGER) FROM control_meta WHERE key = 'schema_version'").Scan(&existingVersion)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read control schema version: %w", err)
		}
		if err == nil && existingVersion > schemaVersion {
			return fmt.Errorf("control database schema %d is newer than supported schema %d", existingVersion, schemaVersion)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS control_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			totp_secret TEXT NOT NULL,
			totp_confirmed INTEGER NOT NULL DEFAULT 0 CHECK (totp_confirmed IN (0, 1)),
			last_totp_step INTEGER NOT NULL DEFAULT -1,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('setup', 'active', 'disabled')),
			auth_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS users_created_at_idx ON users(created_at, id);
	`); err != nil {
		return fmt.Errorf("create control schema: %w", err)
	}
	// Before schema 2, an unconfigured authenticator left the user in a setup
	// state and blocked password-only access. OTP enrollment is now optional and
	// managed from the user's security page, so legacy setup users become active
	// and any unconfirmed enrollment secret is discarded.
	if existingVersion < 2 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET status = 'active', totp_secret = '', last_totp_step = -1
			WHERE status = 'setup' AND totp_confirmed = 0
		`); err != nil {
			return fmt.Errorf("migrate optional TOTP enrollment: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO control_meta(key, value) VALUES('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, schemaVersion); err != nil {
		return fmt.Errorf("record control schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit control schema migration: %w", err)
	}
	return nil
}

func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}
