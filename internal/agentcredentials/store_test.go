package agentcredentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testUserID       = "f20ac70d6a6a4b3c9e1e"
	testOtherUserID  = "a10bc20d3e4f56789012"
	testMasterSecret = "agent-credential-test-secret-with-enough-entropy"
)

func TestStoreEncryptsAndReloadsExaAPIKey(t *testing.T) {
	directory := privateTempDir(t)
	store, err := NewStore(directory, testUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := store.Load()
	if err != nil || credentials.ExaAPIKey != "" {
		t.Fatalf("initial credentials = %#v, error = %v", credentials, err)
	}

	const secret = "exa-secret-value"
	if err := store.Save(Credentials{ExaAPIKey: "  " + secret + "  "}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, credentialsFilename))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("credential file contains the plaintext Exa API key")
	}
	info, err := os.Stat(filepath.Join(directory, credentialsFilename))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential file mode = %o, want 600", info.Mode().Perm())
	}

	reloaded, err := NewStore(directory, testUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err = reloaded.Load()
	if err != nil || credentials.ExaAPIKey != secret {
		t.Fatalf("reloaded credentials = %#v, error = %v", credentials, err)
	}
	fingerprint := reloaded.Fingerprint(credentials)
	if fingerprint == "" || strings.Contains(fingerprint, secret) || fingerprint != store.Fingerprint(credentials) {
		t.Fatalf("credential fingerprint = %q", fingerprint)
	}

	if err := reloaded.Save(Credentials{}); err != nil {
		t.Fatal(err)
	}
	cleared, err := reloaded.Load()
	if err != nil || cleared.ExaAPIKey != "" || reloaded.Fingerprint(cleared) != "" {
		t.Fatalf("cleared credentials = %#v, fingerprint = %q, error = %v", cleared, reloaded.Fingerprint(cleared), err)
	}
}

func TestStoreBindsCredentialsToUserAndMasterSecret(t *testing.T) {
	directory := privateTempDir(t)
	store, err := NewStore(directory, testUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Credentials{ExaAPIKey: "exa-secret-value"}); err != nil {
		t.Fatal(err)
	}

	otherUser, err := NewStore(directory, testOtherUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherUser.Load(); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("other-user load error = %v, want ErrInvalidCredentials", err)
	}
	otherSecret, err := NewStore(directory, testUserID, testMasterSecret+"-different")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherSecret.Load(); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("other-secret load error = %v, want ErrInvalidCredentials", err)
	}
}

func TestStoreRejectsUnsafeFilesAndInvalidKeys(t *testing.T) {
	directory := privateTempDir(t)
	store, err := NewStore(directory, testUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	for _, credentials := range []Credentials{
		{ExaAPIKey: strings.Repeat("x", MaxExaAPIKeyBytes+1)},
		{ExaAPIKey: "exa\nsecret"},
	} {
		if err := store.Save(credentials); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Save(%q) error = %v, want ErrInvalidCredentials", credentials.ExaAPIKey, err)
		}
	}

	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(directory, credentialsFilename)
	if err := os.Symlink(target, credentialPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("symlink load error = %v, want ErrInvalidCredentials", err)
	}
	if err := store.Save(Credentials{ExaAPIKey: "safe"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("symlink save error = %v, want ErrInvalidCredentials", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "untouched" {
		t.Fatalf("symlink target = %q, error = %v", raw, err)
	}
}

func TestStoreRejectsUnknownEnvelopeFieldsAndBroadPermissions(t *testing.T) {
	directory := privateTempDir(t)
	path := filepath.Join(directory, credentialsFilename)
	if err := os.WriteFile(path, []byte(`{"version":1,"nonce":"x","ciphertext":"y","unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(directory, testUserID, testMasterSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown field load error = %v, want ErrInvalidCredentials", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("broad permission load error = %v, want ErrInvalidCredentials", err)
	}
}

func privateTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
