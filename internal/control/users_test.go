package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSessionSecret = "control-store-test-session-secret-with-enough-entropy"

func openTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control", "platform.sqlite")
	db, err := Open(path, testSessionSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestUserIDFormatAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1024)
	for range 1024 {
		id, err := newUserID()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidateUserID(id) || len(id) != UserIDLength {
			t.Fatalf("invalid generated user id %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate generated user id %q", id)
		}
		seen[id] = struct{}{}
	}

	for _, invalid := range []string{
		"", "0123456789abcdef012", "0123456789abcdef01234", "0123456789ABCDEF0123",
		"01234567-9abcdef0123", "g123456789abcdef0123", "../../../../etc/passwd",
	} {
		if ValidateUserID(invalid) {
			t.Fatalf("accepted invalid user id %q", invalid)
		}
	}
}

func TestUserPasswordLoginAndOptionalTOTPEnrollment(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()
	created, err := db.CreateUser(ctx, "测试用户", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateUserID(created.User.ID) || created.User.Status != UserStatusActive || created.User.TOTPConfigured {
		t.Fatalf("unexpected created user: %#v", created.User)
	}
	if created.TOTPSecret != "" || created.TOTPURI != "" {
		t.Fatalf("user creation unexpectedly enrolled TOTP: %#v", created)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "a sufficiently long password") {
		t.Fatal("control database contains a plaintext credential")
	}

	if _, err := db.BeginUserLogin(ctx, created.User.ID, "wrong password"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	login, err := db.BeginUserLogin(ctx, created.User.ID, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if login.User.TOTPConfigured {
		t.Fatalf("unexpected password login: %#v", login)
	}
	if _, err := db.BeginUserTOTPEnrollment(ctx, created.User.ID, "wrong password"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("wrong enrollment password error = %v", err)
	}
	enrollment, err := db.BeginUserTOTPEnrollment(ctx, created.User.ID, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.TOTPSecret == "" || !strings.HasPrefix(enrollment.TOTPURI, "otpauth://totp/") {
		t.Fatalf("missing TOTP enrollment: %#v", enrollment)
	}

	now := time.Unix(1_800_000_000, 0)
	code, err := totpCode(enrollment.TOTPSecret, now.Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.ConfirmUserTOTPEnrollment(ctx, created.User.ID, code, now)
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != UserStatusActive || !user.TOTPConfigured {
		t.Fatalf("verified user = %#v", user)
	}
	if _, err := db.VerifyUserTOTP(ctx, created.User.ID, code, now); !errors.Is(err, ErrTOTPReplay) {
		t.Fatalf("replayed TOTP error = %v", err)
	}
	login, err = db.BeginUserLogin(ctx, created.User.ID, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if !login.User.TOTPConfigured {
		t.Fatalf("confirmed user login = %#v", login)
	}
	if _, err := db.BeginUserTOTPEnrollment(ctx, created.User.ID, "a sufficiently long password"); !errors.Is(err, ErrTOTPAlreadyConfigured) {
		t.Fatalf("duplicate enrollment error = %v", err)
	}
}

func TestUserStatusAndCredentialResetInvalidateSessions(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	enrollment, err := db.CreateUser(ctx, "Owner", "initial password value")
	if err != nil {
		t.Fatal(err)
	}
	initialVersion := enrollment.User.AuthVersion

	disabled, err := db.SetUserDisabled(ctx, enrollment.User.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Status != UserStatusDisabled || disabled.AuthVersion <= initialVersion {
		t.Fatalf("disabled user = %#v", disabled)
	}
	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "initial password value"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled login error = %v", err)
	}

	reset, err := db.ResetUserCredentials(ctx, enrollment.User.ID, "replacement password value")
	if err != nil {
		t.Fatal(err)
	}
	if reset.User.Status != UserStatusActive || reset.User.AuthVersion <= disabled.AuthVersion || reset.User.TOTPConfigured || reset.TOTPSecret != "" {
		t.Fatalf("reset enrollment = %#v", reset)
	}
	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "initial password value"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("old password error = %v", err)
	}
	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "replacement password value"); err != nil {
		t.Fatalf("new password error = %v", err)
	}
}

func TestPasswordChangePreservesAndTOTPRemovalClearsAuthenticator(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	created, err := db.CreateUser(ctx, "Security owner", "initial password value")
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := db.BeginUserTOTPEnrollment(ctx, created.User.ID, "initial password value")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	code, err := totpCode(enrollment.TOTPSecret, now.Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := db.ConfirmUserTOTPEnrollment(ctx, created.User.ID, code, now)
	if err != nil || !configured.TOTPConfigured {
		t.Fatalf("configure TOTP user = %#v, error = %v", configured, err)
	}

	changed, err := db.ChangeUserPassword(ctx, created.User.ID, "initial password value", "replacement password value")
	if err != nil {
		t.Fatal(err)
	}
	if !changed.TOTPConfigured || changed.AuthVersion <= configured.AuthVersion {
		t.Fatalf("changed user = %#v", changed)
	}
	if _, err := db.BeginUserLogin(ctx, created.User.ID, "initial password value"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("old password error = %v", err)
	}
	if _, err := db.DisableUserTOTP(ctx, created.User.ID, "wrong password", code, now); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("wrong removal password error = %v", err)
	}
	removed, err := db.DisableUserTOTP(ctx, created.User.ID, "replacement password value", code, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed.TOTPConfigured || removed.AuthVersion <= changed.AuthVersion {
		t.Fatalf("removed user = %#v", removed)
	}
	login, err := db.BeginUserLogin(ctx, created.User.ID, "replacement password value")
	if err != nil || login.User.TOTPConfigured {
		t.Fatalf("password-only login after removal = %#v, error = %v", login, err)
	}
}

func TestControlDatabaseReopensAndListsUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control", "platform.sqlite")
	db, err := Open(path, testSessionSecret)
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateUser(context.Background(), "Persistent user", "persistent password value")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, testSessionSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	users, err := reopened.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != created.User.ID {
		t.Fatalf("users = %#v", users)
	}
}

func TestSchemaTwoActivatesLegacyUsersWithoutConfirmedTOTP(t *testing.T) {
	db, path := openTestDB(t)
	created, err := db.CreateUser(context.Background(), "Legacy setup user", "legacy password value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`
		UPDATE users SET status = 'setup', totp_secret = 'legacy-unconfirmed-secret' WHERE id = ?;
		UPDATE control_meta SET value = '1' WHERE key = 'schema_version';
	`, created.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, testSessionSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	user, err := reopened.GetUser(context.Background(), created.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != UserStatusActive || user.TOTPConfigured {
		t.Fatalf("migrated user = %#v", user)
	}
	var secret string
	if err := reopened.sql.QueryRow("SELECT totp_secret FROM users WHERE id = ?", created.User.ID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if secret != "" {
		t.Fatalf("legacy unconfirmed secret was retained: %q", secret)
	}
}
