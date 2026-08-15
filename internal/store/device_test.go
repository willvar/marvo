package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestApprovedDeviceTokenIsPrivateAndRevocationPersists(t *testing.T) {
	state, workspace := newTestStateDB(t)
	const secret = "device-test-session-secret"
	devices := NewDeviceStore(state, secret)
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
		t.Fatal(err)
	}
	if strings.Contains(string(publicJSON), approved.Token) || strings.Contains(string(publicJSON), `"token"`) {
		t.Fatalf("admin device listing leaked token: %s", publicJSON)
	}

	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reloaded := NewDeviceStore(reopened, secret)
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
}

func TestDeviceNamesStayUniqueAndRenamingPreservesAuthorization(t *testing.T) {
	state, _ := newTestStateDB(t)
	const secret = "device-name-test-session-secret"
	devices := NewDeviceStore(state, secret)
	approve := func(localDeviceID, name string) *ApprovedDevice {
		t.Helper()
		request, err := devices.CreateRequest(localDeviceID, name, DeviceInfo{})
		if err != nil {
			t.Fatal(err)
		}
		approved, err := devices.ApproveRequest(request.ID)
		if err != nil || approved == nil {
			t.Fatalf("ApproveRequest(%q) = %#v, %v", localDeviceID, approved, err)
		}
		return approved
	}

	first := approve("browser-1", "Café")
	second := approve("browser-2", "CAFÉ")
	if second.DeviceName != "CAFÉ (2)" {
		t.Fatalf("colliding approval name = %q", second.DeviceName)
	}
	if _, err := devices.RenameDevice(second.LocalDeviceID, "Cafe\u0301"); !errors.Is(err, ErrDeviceNameConflict) {
		t.Fatalf("decomposed duplicate error = %v", err)
	}
	if _, err := devices.RenameDevice(second.LocalDeviceID, " "); !errors.Is(err, ErrInvalidDeviceName) {
		t.Fatalf("empty name error = %v", err)
	}
	if _, err := devices.RenameDevice(second.LocalDeviceID, strings.Repeat("名", MaxDeviceNameRunes+1)); !errors.Is(err, ErrInvalidDeviceName) {
		t.Fatalf("long name error = %v", err)
	}

	signature := devices.SignToken(second.Token)
	renamed, err := devices.RenameDevice(second.LocalDeviceID, "  工作平板  ")
	if err != nil || renamed == nil || renamed.DeviceName != "工作平板" || renamed.Token != "" {
		t.Fatalf("RenameDevice() = %#v, %v", renamed, err)
	}
	if !devices.VerifyToken(second.Token, signature) || !devices.VerifyToken(first.Token, devices.SignToken(first.Token)) {
		t.Fatal("renaming invalidated an approved device token")
	}
}

func TestListDevicesSortsByApprovalTimeNewestFirst(t *testing.T) {
	state, _ := newTestStateDB(t)
	devices := NewDeviceStore(state, "device-sort-session-secret")
	approvalTimes := map[string]time.Time{
		"browser-oldest": time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC),
		"browser-newest": time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC),
		"browser-middle": time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC),
	}
	for localDeviceID, approvedAt := range approvalTimes {
		request, err := devices.CreateRequest(localDeviceID, localDeviceID, DeviceInfo{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := devices.ApproveRequest(request.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := state.sql.Exec(`UPDATE devices SET approved_at = ? WHERE local_device_id = ?`, approvedAt.UnixMilli(), localDeviceID); err != nil {
			t.Fatal(err)
		}
	}

	got := devices.ListDevices()
	want := []string{"browser-newest", "browser-middle", "browser-oldest"}
	if len(got) != len(want) {
		t.Fatalf("ListDevices() length = %d, want %d", len(got), len(want))
	}
	for index, localDeviceID := range want {
		if got[index].LocalDeviceID != localDeviceID {
			t.Fatalf("ListDevices()[%d] = %q, want %q", index, got[index].LocalDeviceID, localDeviceID)
		}
	}
}
