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

func TestUserEnrollmentAndTOTPAuthentication(t *testing.T) {
	db, path := openTestDB(t)
	ctx := context.Background()
	enrollment, err := db.CreateUser(ctx, "测试用户", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateUserID(enrollment.User.ID) || enrollment.User.Status != UserStatusSetup {
		t.Fatalf("unexpected enrollment user: %#v", enrollment.User)
	}
	if enrollment.TOTPSecret == "" || !strings.HasPrefix(enrollment.TOTPURI, "otpauth://totp/") {
		t.Fatalf("missing TOTP enrollment: %#v", enrollment)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), enrollment.TOTPSecret) || strings.Contains(string(raw), "a sufficiently long password") {
		t.Fatal("control database contains a plaintext credential")
	}

	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "wrong password"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	challenge, err := db.BeginUserLogin(ctx, enrollment.User.ID, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.User.TOTPConfigured || challenge.TOTPSecret != enrollment.TOTPSecret {
		t.Fatalf("unexpected setup challenge: %#v", challenge)
	}

	now := time.Unix(1_800_000_000, 0)
	code, err := totpCode(enrollment.TOTPSecret, now.Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.VerifyUserTOTP(ctx, enrollment.User.ID, code, now)
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != UserStatusActive || !user.TOTPConfigured {
		t.Fatalf("verified user = %#v", user)
	}
	if _, err := db.VerifyUserTOTP(ctx, enrollment.User.ID, code, now); !errors.Is(err, ErrTOTPReplay) {
		t.Fatalf("replayed TOTP error = %v", err)
	}
	challenge, err = db.BeginUserLogin(ctx, enrollment.User.ID, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.TOTPSecret != "" || challenge.TOTPURI != "" {
		t.Fatalf("confirmed user leaked enrollment secret: %#v", challenge)
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
	if reset.User.Status != UserStatusSetup || reset.User.AuthVersion <= disabled.AuthVersion || reset.TOTPSecret == enrollment.TOTPSecret {
		t.Fatalf("reset enrollment = %#v", reset)
	}
	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "initial password value"); !errors.Is(err, ErrInvalidUserCredentials) {
		t.Fatalf("old password error = %v", err)
	}
	if _, err := db.BeginUserLogin(ctx, enrollment.User.ID, "replacement password value"); err != nil {
		t.Fatalf("new password error = %v", err)
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
