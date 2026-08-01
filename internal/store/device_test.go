package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovedDeviceTokenIsPrivateAndRevocationPersists(t *testing.T) {
	dataDir := t.TempDir()
	const secret = "device-test-session-secret"
	devices := NewDeviceStore(dataDir, secret)
	request, err := devices.CreateRequest("browser-1", "Test browser", DeviceInfo{Platform: "test"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	approved, err := devices.ApproveRequest(request.ID)
	if err != nil || approved == nil || approved.Token == "" {
		t.Fatalf("ApproveRequest() = %#v, %v", approved, err)
	}
	signature := devices.SignToken(approved.Token)
	if !devices.VerifyToken(approved.Token, signature) {
		t.Fatal("newly approved token did not verify")
	}

	publicJSON, err := json.Marshal(devices.ListDevices())
	if err != nil {
		t.Fatalf("Marshal(ListDevices()) error = %v", err)
	}
	if strings.Contains(string(publicJSON), approved.Token) || strings.Contains(string(publicJSON), "\"token\"") {
		t.Fatalf("admin device listing leaked token: %s", publicJSON)
	}
	deviceFile := filepath.Join(dataDir, ".devices.json")
	info, err := os.Stat(deviceFile)
	if err != nil {
		t.Fatalf("Stat(.devices.json) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf(".devices.json permissions = %04o, want 0600", got)
	}

	reloaded := NewDeviceStore(dataDir, secret)
	if !reloaded.VerifyToken(approved.Token, signature) {
		t.Fatal("approved token did not survive restart")
	}
	revoked, err := reloaded.RevokeDevice("browser-1")
	if err != nil || !revoked {
		t.Fatalf("RevokeDevice() = %v, %v", revoked, err)
	}
	if reloaded.VerifyToken(approved.Token, signature) {
		t.Fatal("revoked token still verifies")
	}
	if NewDeviceStore(dataDir, secret).VerifyToken(approved.Token, signature) {
		t.Fatal("revoked token returned after restart")
	}
}
