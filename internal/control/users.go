package control

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type UserStatus string

const (
	UserStatusSetup    UserStatus = "setup"
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

var (
	ErrUserNotFound           = errors.New("user not found")
	ErrInvalidUserCredentials = errors.New("invalid user credentials")
	ErrUserDisabled           = errors.New("user disabled")
	ErrTOTPInvalid            = errors.New("invalid TOTP code")
	ErrTOTPReplay             = errors.New("TOTP code already used")
)

type User struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Status         UserStatus `json:"status"`
	TOTPConfigured bool       `json:"totp_configured"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	AuthVersion    int64      `json:"-"`
}

type Enrollment struct {
	User       User   `json:"user"`
	TOTPSecret string `json:"totp_secret"`
	TOTPURI    string `json:"totp_uri"`
}

type LoginChallenge struct {
	User       User   `json:"user"`
	TOTPSecret string `json:"totp_secret,omitempty"`
	TOTPURI    string `json:"totp_uri,omitempty"`
}

func ValidateUserID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	for index, char := range id {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return id[14] == '4' && (id[19] == '8' || id[19] == '9' || id[19] == 'a' || id[19] == 'b')
}

func newUserID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func ValidateUserName(name string) error {
	if !utf8.ValidString(name) {
		return errors.New("user name must be valid UTF-8")
	}
	if name == "" || name != strings.TrimSpace(name) {
		return errors.New("user name cannot be empty or surrounded by whitespace")
	}
	if len([]rune(name)) > 100 {
		return errors.New("user name is too long")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return errors.New("user name contains control characters")
		}
	}
	return nil
}

func (d *DB) CreateUser(ctx context.Context, name, password string) (*Enrollment, error) {
	name = strings.TrimSpace(name)
	if err := ValidateUserName(name); err != nil {
		return nil, err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}
	encryptedSecret, err := d.encryptTOTPSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	id, err := newUserID()
	if err != nil {
		return nil, fmt.Errorf("generate user id: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO users(
			id, name, password_hash, totp_secret, totp_confirmed,
			last_totp_step, status, auth_version, created_at, updated_at
		) VALUES(?, ?, ?, ?, 0, -1, 'setup', 1, ?, ?)
	`, id, name, passwordHash, encryptedSecret, now.UnixMilli(), now.UnixMilli()); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user := User{
		ID: id, Name: name, Status: UserStatusSetup, TOTPConfigured: false,
		CreatedAt: now, UpdatedAt: now, AuthVersion: 1,
	}
	return &Enrollment{User: user, TOTPSecret: secret, TOTPURI: d.totpURI(name, secret)}, nil
}

func (d *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users
	`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].CreatedAt.Equal(users[j].CreatedAt) {
			return users[i].ID < users[j].ID
		}
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})
	return users, nil
}

func (d *DB) GetUser(ctx context.Context, id string) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrUserNotFound
	}
	row := d.sql.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

func (d *DB) DeleteSetupUser(ctx context.Context, id string) error {
	if !ValidateUserID(id) {
		return ErrUserNotFound
	}
	result, err := d.sql.ExecContext(ctx, `
		DELETE FROM users WHERE id = ? AND status = 'setup' AND totp_confirmed = 0
	`, id)
	if err != nil {
		return fmt.Errorf("delete setup user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete setup user: %w", err)
	}
	if affected != 1 {
		return ErrUserNotFound
	}
	return nil
}

func (d *DB) BeginUserLogin(ctx context.Context, id, password string) (*LoginChallenge, error) {
	if !ValidateUserID(id) {
		return nil, ErrInvalidUserCredentials
	}
	var user User
	var passwordHash string
	var encryptedSecret string
	var confirmed int
	var createdAt int64
	var updatedAt int64
	err := d.sql.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at,
		       password_hash, totp_secret
		FROM users WHERE id = ?
	`, id).Scan(
		&user.ID, &user.Name, &user.Status, &confirmed, &user.AuthVersion, &createdAt, &updatedAt,
		&passwordHash, &encryptedSecret,
	)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !verifyPassword(passwordHash, password) {
		return nil, ErrInvalidUserCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("begin user login: %w", err)
	}
	if user.Status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	user.TOTPConfigured = confirmed == 1
	user.CreatedAt = time.UnixMilli(createdAt).UTC()
	user.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	challenge := &LoginChallenge{User: user}
	if !user.TOTPConfigured {
		secret, err := d.decryptTOTPSecret(encryptedSecret)
		if err != nil {
			return nil, fmt.Errorf("load TOTP enrollment: %w", err)
		}
		challenge.TOTPSecret = secret
		challenge.TOTPURI = d.totpURI(user.Name, secret)
	}
	return challenge, nil
}

func (d *DB) VerifyUserTOTP(ctx context.Context, id, code string, now time.Time) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrTOTPInvalid
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin TOTP verification: %w", err)
	}
	defer tx.Rollback()

	var encryptedSecret string
	var lastStep int64
	var status UserStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT totp_secret, last_totp_step, status FROM users WHERE id = ?
	`, id).Scan(&encryptedSecret, &lastStep, &status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTOTPInvalid
	} else if err != nil {
		return nil, fmt.Errorf("load TOTP state: %w", err)
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	secret, err := d.decryptTOTPSecret(encryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("load TOTP secret: %w", err)
	}
	counter, ok := matchingTOTPCounter(secret, code, now)
	if !ok {
		return nil, ErrTOTPInvalid
	}
	if counter <= lastStep {
		return nil, ErrTOTPReplay
	}
	updatedAt := time.Now().UTC().Truncate(time.Millisecond)
	result, err := tx.ExecContext(ctx, `
		UPDATE users
		SET last_totp_step = ?, totp_confirmed = 1,
		    status = CASE WHEN status = 'setup' THEN 'active' ELSE status END,
		    updated_at = ?
		WHERE id = ? AND last_totp_step < ? AND status != 'disabled'
	`, counter, updatedAt.UnixMilli(), id, counter)
	if err != nil {
		return nil, fmt.Errorf("record TOTP verification: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("record TOTP verification: %w", err)
	}
	if affected != 1 {
		return nil, ErrTOTPReplay
	}
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	user, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("load verified user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit TOTP verification: %w", err)
	}
	return &user, nil
}

func (d *DB) SetUserDisabled(ctx context.Context, id string, disabled bool) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrUserNotFound
	}
	status := UserStatusActive
	if disabled {
		status = UserStatusDisabled
	} else {
		var confirmed int
		if err := d.sql.QueryRowContext(ctx, "SELECT totp_confirmed FROM users WHERE id = ?", id).Scan(&confirmed); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		} else if err != nil {
			return nil, fmt.Errorf("load user state: %w", err)
		}
		if confirmed == 0 {
			status = UserStatusSetup
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	result, err := d.sql.ExecContext(ctx, `
		UPDATE users SET status = ?, auth_version = auth_version + 1, updated_at = ? WHERE id = ?
	`, status, now.UnixMilli(), id)
	if err != nil {
		return nil, fmt.Errorf("update user status: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, ErrUserNotFound
	}
	return d.GetUser(ctx, id)
}

func (d *DB) ResetUserCredentials(ctx context.Context, id, password string) (*Enrollment, error) {
	if !ValidateUserID(id) {
		return nil, ErrUserNotFound
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}
	encryptedSecret, err := d.encryptTOTPSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	result, err := d.sql.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, totp_secret = ?, totp_confirmed = 0,
		    last_totp_step = -1, status = 'setup', auth_version = auth_version + 1,
		    updated_at = ?
		WHERE id = ?
	`, passwordHash, encryptedSecret, now.UnixMilli(), id)
	if err != nil {
		return nil, fmt.Errorf("reset user credentials: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, ErrUserNotFound
	}
	user, err := d.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Enrollment{User: *user, TOTPSecret: secret, TOTPURI: d.totpURI(user.Name, secret)}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var user User
	var confirmed int
	var createdAt int64
	var updatedAt int64
	if err := row.Scan(
		&user.ID, &user.Name, &user.Status, &confirmed, &user.AuthVersion, &createdAt, &updatedAt,
	); err != nil {
		return User{}, err
	}
	user.TOTPConfigured = confirmed == 1
	user.CreatedAt = time.UnixMilli(createdAt).UTC()
	user.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return user, nil
}
