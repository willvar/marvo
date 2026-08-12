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
	space, err := f.registry.Resolve(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
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
	spaceA, _ := fixture.registry.Resolve(context.Background(), userA.User.ID)
	spaceB, _ := fixture.registry.Resolve(context.Background(), userB.User.ID)
	if spaceA.Paths.Workspace == spaceB.Paths.Workspace || spaceA.Hub == spaceB.Hub || spaceA.DeviceStore == spaceB.DeviceStore || spaceA.AgentDeps == spaceB.AgentDeps {
		t.Fatal("user spaces share a stateful dependency")
	}
}

func TestUserAdminSessionIsBoundToUserAndAuthVersion(t *testing.T) {
	fixture := newMultiuserFixture(t)
	userA := fixture.createUser(t, "User A")
	userB := fixture.createUser(t, "User B")

	verify := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/auth/verify", map[string]any{
		"password": "a sufficiently long password",
	})
	if verify.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body = %s", verify.Code, verify.Body.String())
	}
	var challenge struct {
		Token string `json:"challenge_token"`
		TOTP  struct {
			Secret string `json:"secret"`
		} `json:"totp_setup"`
	}
	if err := json.Unmarshal(verify.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	code := testTOTPCode(t, challenge.TOTP.Secret, time.Now())
	login := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/auth", map[string]any{
		"challenge_token": challenge.Token,
		"code":            code,
	})
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == userAdminCookieName(userA.User.ID) {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("user admin session cookie was not set")
	}

	ownAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/devices", nil, session)
	if ownAdmin.Code != http.StatusOK {
		t.Fatalf("own admin status = %d, body = %s", ownAdmin.Code, ownAdmin.Body.String())
	}
	otherAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/admin/devices", nil, session)
	if otherAdmin.Code != http.StatusUnauthorized {
		t.Fatalf("A admin session accessed B with status %d", otherAdmin.Code)
	}

	if _, err := fixture.control.ResetUserCredentials(context.Background(), userA.User.ID, "a replacement password value"); err != nil {
		t.Fatal(err)
	}
	staleAdmin := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/devices", nil, session)
	if staleAdmin.Code != http.StatusUnauthorized {
		t.Fatalf("stale admin session status = %d", staleAdmin.Code)
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
