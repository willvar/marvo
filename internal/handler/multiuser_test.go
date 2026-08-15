package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"marvo/config"
	"marvo/internal/control"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

const multiuserTestSecret = "multiuser-handler-test-session-secret-long-enough"

type multiuserFixture struct {
	deps     *Dependencies
	mux      *http.ServeMux
	control  *control.DB
	registry *SpaceRegistry
}

func newMultiuserFixture(t *testing.T) *multiuserFixture {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	layout, err := userspace.OpenLayout(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	controlDB, err := control.Open(layout.ControlDatabase(), multiuserTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	shutdown := make(chan struct{})
	cfg := &config.Config{
		Server:   config.ServerConfig{StateDir: stateRoot, DataDir: t.TempDir(), SessionSecret: multiuserTestSecret},
		OpenCode: config.OpenCodeConfig{URL: "http://127.0.0.1:1"},
		Runtime:  config.RuntimeConfig{URL: "http://runtime.invalid", Token: multiuserTestSecret},
	}
	registry := NewSpaceRegistry(cfg, controlDB, layout, shutdown)
	deps := &Dependencies{Config: cfg, Control: controlDB, Layout: layout, Spaces: registry}
	mux := http.NewServeMux()
	RegisterRoutes(mux, deps)
	t.Cleanup(func() {
		close(shutdown)
		registry.Close()
		_ = controlDB.Close()
	})
	return &multiuserFixture{deps: deps, mux: mux, control: controlDB, registry: registry}
}

func (f *multiuserFixture) createUser(t *testing.T, name string) *control.Enrollment {
	t.Helper()
	enrollment, err := f.control.CreateUser(context.Background(), name, "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	return enrollment
}

func (f *multiuserFixture) approveDevice(t *testing.T, userID, localID string) *http.Cookie {
	t.Helper()
	space := f.space(t, userID)
	pending, err := space.DeviceStore.CreateRequest(localID, localID, store.DeviceInfo{})
	if err != nil {
		t.Fatal(err)
	}
	device, err := space.DeviceStore.ApproveRequest(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{
		Name:  userDeviceCookieName(userID),
		Value: device.Token + ":" + space.DeviceStore.SignToken(device.Token),
	}
}

func (f *multiuserFixture) space(t *testing.T, userID string) *UserSpace {
	t.Helper()
	space, release, err := f.registry.Acquire(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return space
}

func TestPublicUserIdentityOnlyExposesTheSpaceName(t *testing.T) {
	fixture := newMultiuserFixture(t)
	user := fixture.createUser(t, "User A")

	identity := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+user.User.ID+"/identity", nil)
	if identity.Code != http.StatusOK {
		t.Fatalf("public identity status = %d, body = %s", identity.Code, identity.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(identity.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload["name"] != "User A" {
		t.Fatalf("public identity exposed unexpected data: %#v", payload)
	}

	missing := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/00000000000000000000/identity", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing public identity status = %d, body = %s", missing.Code, missing.Body.String())
	}
}

func serveJSON(t *testing.T, handler http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &payload)
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestUserRoutesKeepNotesDevicesAndCookiesIsolated(t *testing.T) {
	fixture := newMultiuserFixture(t)
	userA := fixture.createUser(t, "User A")
	userB := fixture.createUser(t, "User B")
	cookieA := fixture.approveDevice(t, userA.User.ID, "browser-a")
	cookieB := fixture.approveDevice(t, userB.User.ID, "browser-b")

	createA := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/notes", map[string]any{
		"title": "Shared title", "content": "A-only content", "tags": []string{},
	}, cookieA)
	if createA.Code != http.StatusCreated {
		t.Fatalf("create A status = %d, body = %s", createA.Code, createA.Body.String())
	}
	createB := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userB.User.ID+"/notes", map[string]any{
		"title": "Shared title", "content": "B-only content", "tags": []string{},
	}, cookieB)
	if createB.Code != http.StatusCreated {
		t.Fatalf("create B status = %d, body = %s", createB.Code, createB.Body.String())
	}

	readA := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/notes/Shared%20title", nil, cookieA)
	if readA.Code != http.StatusOK || !bytes.Contains(readA.Body.Bytes(), []byte("A-only content")) || bytes.Contains(readA.Body.Bytes(), []byte("B-only content")) {
		t.Fatalf("read A status = %d, body = %s", readA.Code, readA.Body.String())
	}
	readB := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/notes/Shared%20title", nil, cookieB)
	if readB.Code != http.StatusOK || !bytes.Contains(readB.Body.Bytes(), []byte("B-only content")) || bytes.Contains(readB.Body.Bytes(), []byte("A-only content")) {
		t.Fatalf("read B status = %d, body = %s", readB.Code, readB.Body.String())
	}

	crossed := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/notes", nil, cookieA)
	if crossed.Code != http.StatusUnauthorized {
		t.Fatalf("A cookie accessed B with status %d", crossed.Code)
	}
	spaceA := fixture.space(t, userA.User.ID)
	spaceB := fixture.space(t, userB.User.ID)
	if spaceA.Paths.Workspace == spaceB.Paths.Workspace || spaceA.Hub == spaceB.Hub || spaceA.DeviceStore == spaceB.DeviceStore || spaceA.AgentDeps == spaceB.AgentDeps {
		t.Fatal("user spaces share a stateful dependency")
	}
	if info, err := os.Stat(spaceA.State.Path()); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private user state database = %#v, %v", info, err)
	}
	if filepath.Dir(filepath.Dir(spaceA.State.Path())) != spaceA.Paths.Workspace {
		t.Fatalf("user state database escaped workspace: %s", spaceA.State.Path())
	}
	activityA, _, err := spaceA.Activity.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "A-only activity", Content: "Only A can read this.",
		SourceSessionID: "session-a", SourceMessageID: "message-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := spaceB.Activity.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "B-only activity", Content: "Only B can read this.",
		SourceSessionID: "session-b", SourceMessageID: "message-b",
	}); err != nil {
		t.Fatal(err)
	}
	activityListA := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/activity", nil, cookieA)
	if activityListA.Code != http.StatusOK || !bytes.Contains(activityListA.Body.Bytes(), []byte(activityA.ID)) || bytes.Contains(activityListA.Body.Bytes(), []byte("B-only activity")) {
		t.Fatalf("user A activity status = %d, body = %s", activityListA.Code, activityListA.Body.String())
	}
	crossedActivities := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/activity", nil, cookieA)
	if crossedActivities.Code != http.StatusUnauthorized {
		t.Fatalf("A cookie accessed B activities with status %d", crossedActivities.Code)
	}
	if _, err := os.Lstat(filepath.Join(spaceA.Paths.Root, "app")); !os.IsNotExist(err) {
		t.Fatalf("retired app directory exists or cannot be inspected: %v", err)
	}
}

func TestUserAdminSessionIsBoundToUserAndAuthVersion(t *testing.T) {
	fixture := newMultiuserFixture(t)
	userA := fixture.createUser(t, "User A")
	userB := fixture.createUser(t, "User B")
	deviceSession := fixture.approveDevice(t, userA.User.ID, "browser-a")
	fixture.approveDevice(t, userA.User.ID, "browser-b")

	verify := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/auth/verify", map[string]any{
		"password": "a sufficiently long password",
	})
	if verify.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body = %s", verify.Code, verify.Body.String())
	}
	if !bytes.Contains(verify.Body.Bytes(), []byte(`"authenticated":true`)) {
		t.Fatalf("password-only login response = %s", verify.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range verify.Result().Cookies() {
		if cookie.Name == userAdminCookieName(userA.User.ID) {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("user admin session cookie was not set")
	}

	identity := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/me", nil, session)
	if identity.Code != http.StatusOK || !bytes.Contains(identity.Body.Bytes(), []byte(`"name":"User A"`)) {
		t.Fatalf("user admin identity status = %d, body = %s", identity.Code, identity.Body.String())
	}
	crossedIdentity := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/admin/me", nil, session)
	if crossedIdentity.Code != http.StatusUnauthorized {
		t.Fatalf("A admin session read B identity with status %d", crossedIdentity.Code)
	}
	deviceOnlySettings := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/agent/memories", nil, deviceSession)
	if deviceOnlySettings.Code != http.StatusUnauthorized {
		t.Fatalf("device session accessed agent settings with status %d", deviceOnlySettings.Code)
	}
	adminSettings := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/agent/memories", nil, session)
	if adminSettings.Code != http.StatusOK {
		t.Fatalf("admin session agent settings status = %d, body = %s", adminSettings.Code, adminSettings.Body.String())
	}

	ownAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/devices", nil, session)
	if ownAdmin.Code != http.StatusOK {
		t.Fatalf("own admin status = %d, body = %s", ownAdmin.Code, ownAdmin.Body.String())
	}
	renamedDevice := serveJSON(t, fixture.mux, http.MethodPatch, "/api/user/"+userA.User.ID+"/admin/devices/browser-a", map[string]any{
		"device_name": "  Work tablet  ",
	}, session)
	if renamedDevice.Code != http.StatusOK || !bytes.Contains(renamedDevice.Body.Bytes(), []byte(`"device_name":"Work tablet"`)) {
		t.Fatalf("rename device status = %d, body = %s", renamedDevice.Code, renamedDevice.Body.String())
	}
	duplicateName := serveJSON(t, fixture.mux, http.MethodPatch, "/api/user/"+userA.User.ID+"/admin/devices/browser-b", map[string]any{
		"device_name": "WORK TABLET",
	}, session)
	if duplicateName.Code != http.StatusConflict {
		t.Fatalf("duplicate device name status = %d, body = %s", duplicateName.Code, duplicateName.Body.String())
	}
	otherAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/admin/devices", nil, session)
	if otherAdmin.Code != http.StatusUnauthorized {
		t.Fatalf("A admin session accessed B with status %d", otherAdmin.Code)
	}
	crossedRename := serveJSON(t, fixture.mux, http.MethodPatch, "/api/user/"+userB.User.ID+"/admin/devices/browser-a", map[string]any{
		"device_name": "Crossed",
	}, session)
	if crossedRename.Code != http.StatusUnauthorized {
		t.Fatalf("A admin session renamed B device with status %d", crossedRename.Code)
	}

	updatedBrand := serveJSON(t, fixture.mux, http.MethodPut, "/api/user/"+userA.User.ID+"/admin/brand", map[string]any{
		"name": "User A Notes",
	}, session)
	if updatedBrand.Code != http.StatusOK || !bytes.Contains(updatedBrand.Body.Bytes(), []byte("User A Notes")) {
		t.Fatalf("brand update status = %d, body = %s", updatedBrand.Code, updatedBrand.Body.String())
	}
	crossedBrand := serveJSON(t, fixture.mux, http.MethodPut, "/api/user/"+userB.User.ID+"/admin/brand", map[string]any{
		"name": "Crossed",
	}, session)
	if crossedBrand.Code != http.StatusUnauthorized {
		t.Fatalf("A admin session changed B brand with status %d", crossedBrand.Code)
	}
	spaceA := fixture.space(t, userA.User.ID)
	if spaceA.BrandStore.Get().Name != "User A Notes" {
		t.Fatalf("stored brand = %#v", spaceA.BrandStore.Get())
	}
	spaceB := fixture.space(t, userB.User.ID)
	if spaceB.BrandStore.Get().Name != store.DefaultBrandName {
		t.Fatalf("B brand = %#v", spaceB.BrandStore.Get())
	}
	if err := os.WriteFile(filepath.Join(spaceA.Paths.Workspace, "usage-probe.bin"), bytes.Repeat([]byte("x"), 4096), 0600); err != nil {
		t.Fatal(err)
	}
	spaceInfo := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/space", nil, session)
	if spaceInfo.Code != http.StatusOK {
		t.Fatalf("space info status = %d, body = %s", spaceInfo.Code, spaceInfo.Body.String())
	}
	var usage struct {
		Space struct {
			UsedBytes     int64  `json:"used_bytes"`
			CapacityBytes *int64 `json:"capacity_bytes"`
		} `json:"space"`
	}
	if err := json.Unmarshal(spaceInfo.Body.Bytes(), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Space.UsedBytes < 4096 || usage.Space.CapacityBytes != nil {
		t.Fatalf("space usage = %#v", usage.Space)
	}

	security := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/security", nil, session)
	if security.Code != http.StatusOK || !bytes.Contains(security.Body.Bytes(), []byte(`"totp_configured":false`)) {
		t.Fatalf("security status = %d, body = %s", security.Code, security.Body.String())
	}
	beginTOTP := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/admin/security/totp", map[string]any{
		"password": "a sufficiently long password",
	}, session)
	if beginTOTP.Code != http.StatusOK {
		t.Fatalf("begin TOTP status = %d, body = %s", beginTOTP.Code, beginTOTP.Body.String())
	}
	var setup struct {
		TOTP struct {
			Secret string `json:"secret"`
		} `json:"totp_setup"`
	}
	if err := json.Unmarshal(beginTOTP.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	confirmTOTP := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/admin/security/totp/confirm", map[string]any{
		"code": testTOTPCode(t, setup.TOTP.Secret, time.Now()),
	}, session)
	if confirmTOTP.Code != http.StatusOK {
		t.Fatalf("confirm TOTP status = %d, body = %s", confirmTOTP.Code, confirmTOTP.Body.String())
	}
	var configuredSession *http.Cookie
	for _, cookie := range confirmTOTP.Result().Cookies() {
		if cookie.Name == userAdminCookieName(userA.User.ID) {
			configuredSession = cookie
		}
	}
	if configuredSession == nil {
		t.Fatal("TOTP confirmation did not refresh the user session")
	}
	staleAfterTOTPSetup := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/security", nil, session)
	if staleAfterTOTPSetup.Code != http.StatusUnauthorized {
		t.Fatalf("pre-TOTP-setup session status = %d", staleAfterTOTPSetup.Code)
	}
	changedPassword := "a changed password value"
	change := serveJSON(t, fixture.mux, http.MethodPut, "/api/user/"+userA.User.ID+"/admin/security/password", map[string]any{
		"current_password": "a sufficiently long password",
		"new_password":     changedPassword,
	}, configuredSession)
	if change.Code != http.StatusOK {
		t.Fatalf("password change status = %d, body = %s", change.Code, change.Body.String())
	}
	var changedSession *http.Cookie
	for _, cookie := range change.Result().Cookies() {
		if cookie.Name == userAdminCookieName(userA.User.ID) {
			changedSession = cookie
		}
	}
	if changedSession == nil {
		t.Fatal("password change did not refresh the user session")
	}
	staleAfterPasswordChange := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/security", nil, session)
	if staleAfterPasswordChange.Code != http.StatusUnauthorized {
		t.Fatalf("pre-password-change session status = %d", staleAfterPasswordChange.Code)
	}
	configuredSecurity := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/security", nil, changedSession)
	if configuredSecurity.Code != http.StatusOK || !bytes.Contains(configuredSecurity.Body.Bytes(), []byte(`"totp_configured":true`)) {
		t.Fatalf("configured security status = %d, body = %s", configuredSecurity.Code, configuredSecurity.Body.String())
	}
	removeTOTP := serveJSON(t, fixture.mux, http.MethodDelete, "/api/user/"+userA.User.ID+"/admin/security/totp", map[string]any{
		"password": changedPassword,
		"code":     testTOTPCode(t, setup.TOTP.Secret, time.Now()),
	}, changedSession)
	if removeTOTP.Code != http.StatusOK {
		t.Fatalf("remove TOTP status = %d, body = %s", removeTOTP.Code, removeTOTP.Body.String())
	}
	var unconfiguredSession *http.Cookie
	for _, cookie := range removeTOTP.Result().Cookies() {
		if cookie.Name == userAdminCookieName(userA.User.ID) {
			unconfiguredSession = cookie
		}
	}
	if unconfiguredSession == nil {
		t.Fatal("TOTP removal did not refresh the user session")
	}
	passwordOnlyAgain := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/auth/verify", map[string]any{
		"password": changedPassword,
	})
	if passwordOnlyAgain.Code != http.StatusOK || !bytes.Contains(passwordOnlyAgain.Body.Bytes(), []byte(`"authenticated":true`)) {
		t.Fatalf("password-only login after TOTP removal = %d, body = %s", passwordOnlyAgain.Code, passwordOnlyAgain.Body.String())
	}

	if _, err := fixture.control.ResetUserCredentials(context.Background(), userA.User.ID, "a replacement password value"); err != nil {
		t.Fatal(err)
	}
	staleAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/devices", nil, unconfiguredSession)
	if staleAdmin.Code != http.StatusUnauthorized {
		t.Fatalf("stale admin session status = %d", staleAdmin.Code)
	}
}

func TestConfiguredTOTPRequiresAndAcceptsSecondLoginStep(t *testing.T) {
	fixture := newMultiuserFixture(t)
	user := fixture.createUser(t, "TOTP user")
	enrollment, err := fixture.control.BeginUserTOTPEnrollment(
		context.Background(), user.User.ID, "a sufficiently long password",
	)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if _, err := fixture.control.ConfirmUserTOTPEnrollment(
		context.Background(), user.User.ID, testTOTPCode(t, enrollment.TOTPSecret, past), past,
	); err != nil {
		t.Fatal(err)
	}

	verify := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+user.User.ID+"/auth/verify", map[string]any{
		"password": "a sufficiently long password",
	})
	if verify.Code != http.StatusOK || !bytes.Contains(verify.Body.Bytes(), []byte(`"authenticated":false`)) {
		t.Fatalf("TOTP verify response = %d, body = %s", verify.Code, verify.Body.String())
	}
	if len(verify.Result().Cookies()) != 0 {
		t.Fatal("password step unexpectedly created an administrator session")
	}
	var challenge struct {
		Token string `json:"challenge_token"`
	}
	if err := json.Unmarshal(verify.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	login := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+user.User.ID+"/auth", map[string]any{
		"challenge_token": challenge.Token,
		"code":            testTOTPCode(t, enrollment.TOTPSecret, time.Now()),
	})
	if login.Code != http.StatusOK {
		t.Fatalf("TOTP login response = %d, body = %s", login.Code, login.Body.String())
	}
	foundSession := false
	for _, cookie := range login.Result().Cookies() {
		foundSession = foundSession || cookie.Name == userAdminCookieName(user.User.ID)
	}
	if !foundSession {
		t.Fatal("TOTP login did not create an administrator session")
	}
}

func testTOTPCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}
