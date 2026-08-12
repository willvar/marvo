package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"marvo/internal/userid"
)

type UserStatus string

const (
	UserIDLength      = userid.Length
	maxUserIDAttempts = 8

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
	ErrTOTPAlreadyConfigured  = errors.New("TOTP is already configured")
	ErrTOTPNotConfigured      = errors.New("TOTP is not configured")
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
	User User `json:"user"`
}

func ValidateUserID(id string) bool {
	return userid.Valid(id)
}

func newUserID() (string, error) {
	return userid.New()
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
	now := time.Now().UTC().Truncate(time.Millisecond)
	for attempt := 0; attempt < maxUserIDAttempts; attempt++ {
		id, err := newUserID()
		if err != nil {
			return nil, fmt.Errorf("generate user id: %w", err)
		}
		result, err := d.sql.ExecContext(ctx, `
			INSERT INTO users(
				id, name, password_hash, totp_secret, totp_confirmed,
				last_totp_step, status, auth_version, created_at, updated_at
			) VALUES(?, ?, ?, '', 0, -1, 'active', 1, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, id, name, passwordHash, now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("check user creation: %w", err)
		}
		if affected == 0 {
			continue
		}
		user := User{
			ID: id, Name: name, Status: UserStatusActive, TOTPConfigured: false,
			CreatedAt: now, UpdatedAt: now, AuthVersion: 1,
		}
		return &Enrollment{User: user}, nil
	}
	return nil, errors.New("failed to allocate a unique user id")
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
		DELETE FROM users
		WHERE id = ? AND auth_version = 1 AND totp_confirmed = 0 AND created_at = updated_at
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
	var confirmed int
	var createdAt int64
	var updatedAt int64
	err := d.sql.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at,
		       password_hash
		FROM users WHERE id = ?
	`, id).Scan(
		&user.ID, &user.Name, &user.Status, &confirmed, &user.AuthVersion, &createdAt, &updatedAt,
		&passwordHash,
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
	return &LoginChallenge{User: user}, nil
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
	var confirmed int
	if err := tx.QueryRowContext(ctx, `
		SELECT totp_secret, last_totp_step, status, totp_confirmed FROM users WHERE id = ?
	`, id).Scan(&encryptedSecret, &lastStep, &status, &confirmed); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTOTPInvalid
	} else if err != nil {
		return nil, fmt.Errorf("load TOTP state: %w", err)
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	if confirmed != 1 {
		return nil, ErrTOTPNotConfigured
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
		SET last_totp_step = ?, updated_at = ?
		WHERE id = ? AND last_totp_step < ? AND totp_confirmed = 1 AND status != 'disabled'
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

func (d *DB) ConfirmUserTOTPEnrollment(ctx context.Context, id, code string, now time.Time) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrTOTPInvalid
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin TOTP confirmation: %w", err)
	}
	defer tx.Rollback()

	var encryptedSecret string
	var lastStep int64
	var status UserStatus
	var confirmed int
	if err := tx.QueryRowContext(ctx, `
		SELECT totp_secret, last_totp_step, status, totp_confirmed FROM users WHERE id = ?
	`, id).Scan(&encryptedSecret, &lastStep, &status, &confirmed); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTOTPInvalid
	} else if err != nil {
		return nil, fmt.Errorf("load TOTP confirmation state: %w", err)
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	if confirmed == 1 {
		return nil, ErrTOTPAlreadyConfigured
	}
	if encryptedSecret == "" {
		return nil, ErrTOTPNotConfigured
	}
	secret, err := d.decryptTOTPSecret(encryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("load TOTP enrollment secret: %w", err)
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
		SET last_totp_step = ?, totp_confirmed = 1, status = 'active',
		    auth_version = auth_version + 1, updated_at = ?
		WHERE id = ? AND last_totp_step < ? AND totp_confirmed = 0 AND status != 'disabled'
	`, counter, updatedAt.UnixMilli(), id, counter)
	if err != nil {
		return nil, fmt.Errorf("confirm TOTP enrollment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("confirm TOTP enrollment: %w", err)
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
		return nil, fmt.Errorf("load confirmed TOTP user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit TOTP confirmation: %w", err)
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
	now := time.Now().UTC().Truncate(time.Millisecond)
	result, err := d.sql.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, totp_secret = '', totp_confirmed = 0,
		    last_totp_step = -1, status = 'active', auth_version = auth_version + 1,
		    updated_at = ?
		WHERE id = ?
	`, passwordHash, now.UnixMilli(), id)
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
	return &Enrollment{User: *user}, nil
}

func (d *DB) ChangeUserPassword(ctx context.Context, id, currentPassword, newPassword string) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrInvalidUserCredentials
	}
	newHash, err := hashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin password change: %w", err)
	}
	defer tx.Rollback()

	var currentHash string
	var status UserStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT password_hash, status FROM users WHERE id = ?
	`, id).Scan(&currentHash, &status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidUserCredentials
	} else if err != nil {
		return nil, fmt.Errorf("load password state: %w", err)
	}
	if !verifyPassword(currentHash, currentPassword) {
		return nil, ErrInvalidUserCredentials
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, auth_version = auth_version + 1, updated_at = ?
		WHERE id = ?
	`, newHash, now.UnixMilli(), id); err != nil {
		return nil, fmt.Errorf("change password: %w", err)
	}
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	user, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("load changed user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit password change: %w", err)
	}
	return &user, nil
}

func (d *DB) BeginUserTOTPEnrollment(ctx context.Context, id, password string) (*Enrollment, error) {
	if !ValidateUserID(id) {
		return nil, ErrInvalidUserCredentials
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}
	encryptedSecret, err := d.encryptTOTPSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin TOTP enrollment: %w", err)
	}
	defer tx.Rollback()

	var currentHash string
	var status UserStatus
	var confirmed int
	if err := tx.QueryRowContext(ctx, `
		SELECT password_hash, status, totp_confirmed FROM users WHERE id = ?
	`, id).Scan(&currentHash, &status, &confirmed); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidUserCredentials
	} else if err != nil {
		return nil, fmt.Errorf("load TOTP enrollment state: %w", err)
	}
	if !verifyPassword(currentHash, password) {
		return nil, ErrInvalidUserCredentials
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	if confirmed == 1 {
		return nil, ErrTOTPAlreadyConfigured
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET totp_secret = ?, last_totp_step = -1, updated_at = ?
		WHERE id = ? AND totp_confirmed = 0
	`, encryptedSecret, now.UnixMilli(), id); err != nil {
		return nil, fmt.Errorf("store TOTP enrollment: %w", err)
	}
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	user, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("load TOTP enrollment user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit TOTP enrollment: %w", err)
	}
	return &Enrollment{User: user, TOTPSecret: secret, TOTPURI: d.totpURI(user.Name, secret)}, nil
}

func (d *DB) DisableUserTOTP(ctx context.Context, id, password, code string, now time.Time) (*User, error) {
	if !ValidateUserID(id) {
		return nil, ErrInvalidUserCredentials
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin TOTP removal: %w", err)
	}
	defer tx.Rollback()

	var currentHash string
	var encryptedSecret string
	var status UserStatus
	var confirmed int
	if err := tx.QueryRowContext(ctx, `
		SELECT password_hash, totp_secret, status, totp_confirmed
		FROM users WHERE id = ?
	`, id).Scan(&currentHash, &encryptedSecret, &status, &confirmed); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidUserCredentials
	} else if err != nil {
		return nil, fmt.Errorf("load TOTP removal state: %w", err)
	}
	if !verifyPassword(currentHash, password) {
		return nil, ErrInvalidUserCredentials
	}
	if status == UserStatusDisabled {
		return nil, ErrUserDisabled
	}
	if confirmed != 1 {
		return nil, ErrTOTPNotConfigured
	}
	secret, err := d.decryptTOTPSecret(encryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("load TOTP secret: %w", err)
	}
	if _, ok := matchingTOTPCounter(secret, code, now); !ok {
		return nil, ErrTOTPInvalid
	}
	updatedAt := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET totp_secret = '', totp_confirmed = 0, last_totp_step = -1,
		    auth_version = auth_version + 1, updated_at = ?
		WHERE id = ? AND totp_confirmed = 1
	`, updatedAt.UnixMilli(), id); err != nil {
		return nil, fmt.Errorf("remove TOTP: %w", err)
	}
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, status, totp_confirmed, auth_version, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	user, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("load TOTP removal user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit TOTP removal: %w", err)
	}
	return &user, nil
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
