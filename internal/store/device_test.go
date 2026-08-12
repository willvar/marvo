package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDeviceNamesStayUniqueAndRenamingPreservesAuthorization(t *testing.T) {
	dataDir := t.TempDir()
	const secret = "device-name-test-session-secret"
	devices := NewDeviceStore(dataDir, secret)

	approve := func(localDeviceID, name string) *ApprovedDevice {
		t.Helper()
		request, err := devices.CreateRequest(localDeviceID, name, DeviceInfo{})
		if err != nil {
			t.Fatalf("CreateRequest(%q, %q) error = %v", localDeviceID, name, err)
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
		t.Fatalf("colliding approval name = %q, want %q", second.DeviceName, "CAFÉ (2)")
	}

	if _, err := devices.RenameDevice(second.LocalDeviceID, "Cafe\u0301"); !errors.Is(err, ErrDeviceNameConflict) {
		t.Fatalf("RenameDevice() decomposed duplicate error = %v, want ErrDeviceNameConflict", err)
	}
	if _, err := devices.RenameDevice(second.LocalDeviceID, " "); !errors.Is(err, ErrInvalidDeviceName) {
		t.Fatalf("RenameDevice() empty error = %v, want ErrInvalidDeviceName", err)
	}
	if _, err := devices.RenameDevice(second.LocalDeviceID, strings.Repeat("名", MaxDeviceNameRunes+1)); !errors.Is(err, ErrInvalidDeviceName) {
		t.Fatalf("RenameDevice() long error = %v, want ErrInvalidDeviceName", err)
	}

	signature := devices.SignToken(second.Token)
	renamed, err := devices.RenameDevice(second.LocalDeviceID, "  工作平板  ")
	if err != nil || renamed == nil {
		t.Fatalf("RenameDevice() = %#v, %v", renamed, err)
	}
	if renamed.DeviceName != "工作平板" || renamed.Token != "" {
		t.Fatalf("renamed public device = %#v", renamed)
	}
	if !devices.VerifyToken(second.Token, signature) {
		t.Fatal("renaming invalidated the approved device token")
	}
	if !devices.VerifyToken(first.Token, devices.SignToken(first.Token)) {
		t.Fatal("renaming another device invalidated the first token")
	}

	reloaded := NewDeviceStore(dataDir, secret)
	listed := reloaded.ListDevices()
	if len(listed) != 2 {
		t.Fatalf("reloaded device count = %d, want 2", len(listed))
	}
	for _, device := range listed {
		if device.LocalDeviceID == second.LocalDeviceID && device.DeviceName != "工作平板" {
			t.Fatalf("persisted device name = %q, want 工作平板", device.DeviceName)
		}
	}
}

func TestLoadingExistingDevicesNormalizesDuplicateNames(t *testing.T) {
	dataDir := t.TempDir()
	deviceFilePath := filepath.Join(dataDir, ".devices.json")
	oldest := time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC)
	newest := oldest.Add(time.Hour)
	contents := deviceFile{
		Pending: map[string]*PendingRequest{},
		Approved: map[string]*approvedDeviceRecord{
			"browser-oldest": {
				ID: "device-oldest", LocalDeviceID: "browser-oldest", DeviceName: " Café ",
				Token: "token-oldest", ApprovedAt: oldest,
			},
			"browser-newest": {
				ID: "device-newest", LocalDeviceID: "browser-newest", DeviceName: "CAFE\u0301",
				Token: "token-newest", ApprovedAt: newest,
			},
		},
	}
	encoded, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deviceFilePath, encoded, 0600); err != nil {
		t.Fatal(err)
	}

	devices := NewDeviceStore(dataDir, "device-name-migration-secret")
	listed := devices.ListDevices()
	if len(listed) != 2 {
		t.Fatalf("ListDevices() length = %d, want 2", len(listed))
	}
	names := make(map[string]string, len(listed))
	for _, device := range listed {
		names[device.LocalDeviceID] = device.DeviceName
	}
	if names["browser-oldest"] != "Café" || names["browser-newest"] != "CAFÉ (2)" {
		t.Fatalf("normalized loaded names = %#v", names)
	}

	reloaded := NewDeviceStore(dataDir, "device-name-migration-secret")
	reloadedNames := make(map[string]string)
	for _, device := range reloaded.ListDevices() {
		reloadedNames[device.LocalDeviceID] = device.DeviceName
	}
	if reloadedNames["browser-oldest"] != "Café" || reloadedNames["browser-newest"] != "CAFÉ (2)" {
		t.Fatalf("persisted normalized names = %#v", reloadedNames)
	}
}

func TestListDevicesSortsByApprovalTimeNewestFirst(t *testing.T) {
	devices := NewDeviceStore(t.TempDir(), "device-sort-session-secret")
	approvalTimes := map[string]time.Time{
		"browser-oldest": time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC),
		"browser-newest": time.Date(2026, time.August, 11, 8, 0, 0, 0, time.UTC),
		"browser-middle": time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC),
	}
	for localDeviceID, approvedAt := range approvalTimes {
		request, err := devices.CreateRequest(localDeviceID, localDeviceID, DeviceInfo{})
		if err != nil {
			t.Fatalf("CreateRequest(%q) error = %v", localDeviceID, err)
		}
		approved, err := devices.ApproveRequest(request.ID)
		if err != nil || approved == nil {
			t.Fatalf("ApproveRequest(%q) = %#v, %v", localDeviceID, approved, err)
		}
		devices.approved[localDeviceID].ApprovedAt = approvedAt
	}

	got := devices.ListDevices()
	want := []string{"browser-newest", "browser-middle", "browser-oldest"}
	if len(got) != len(want) {
		t.Fatalf("ListDevices() length = %d, want %d", len(got), len(want))
	}
	for i, localDeviceID := range want {
		if got[i].LocalDeviceID != localDeviceID {
			t.Fatalf("ListDevices()[%d].LocalDeviceID = %q, want %q", i, got[i].LocalDeviceID, localDeviceID)
		}
	}
}
