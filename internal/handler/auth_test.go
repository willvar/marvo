package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marvo/config"
	"marvo/internal/store"
)

func TestAdminAndApprovedDeviceSessionsAreStrictlySeparated(t *testing.T) {
	const secret = "handler-auth-test-secret"
	deviceStore := store.NewDeviceStore(t.TempDir(), secret)
	deps := &Dependencies{
		Config:      &config.Config{Server: config.ServerConfig{SessionSecret: secret}},
		DeviceStore: deviceStore,
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	adminValue := fmt.Sprintf("marvo:%d:test-challenge", time.Now().Unix())
	adminCookie := &http.Cookie{Name: "marvo_session", Value: adminValue + ":" + signPayload(adminValue, secret)}
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	adminRequest.AddCookie(adminCookie)
	adminResponse := httptest.NewRecorder()
	deps.AuthMiddleware()(ok).ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusUnauthorized {
		t.Fatalf("admin-only cookie read content with status %d", adminResponse.Code)
	}
	adminCheck := httptest.NewRecorder()
	deps.AdminMiddleware()(ok).ServeHTTP(adminCheck, adminRequest)
	if adminCheck.Code != http.StatusNoContent {
		t.Fatalf("valid admin cookie status = %d", adminCheck.Code)
	}

	pending, err := deviceStore.CreateRequest("approved-browser", "Approved browser", store.DeviceInfo{})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := deviceStore.ApproveRequest(pending.ID)
	if err != nil || approved == nil {
		t.Fatalf("ApproveRequest() = %#v, %v", approved, err)
	}
	deviceCookie := &http.Cookie{
		Name:  "marvo_device",
		Value: approved.Token + ":" + deviceStore.SignToken(approved.Token),
	}
	deviceRequest := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	deviceRequest.AddCookie(deviceCookie)
	deviceResponse := httptest.NewRecorder()
	deps.AuthMiddleware()(ok).ServeHTTP(deviceResponse, deviceRequest)
	if deviceResponse.Code != http.StatusNoContent {
		t.Fatalf("approved device status = %d", deviceResponse.Code)
	}
	deviceAdminResponse := httptest.NewRecorder()
	deps.AdminMiddleware()(ok).ServeHTTP(deviceAdminResponse, deviceRequest)
	if deviceAdminResponse.Code != http.StatusUnauthorized {
		t.Fatalf("device-only cookie accessed admin route with status %d", deviceAdminResponse.Code)
	}

	if revoked, err := deviceStore.RevokeDevice("approved-browser"); err != nil || !revoked {
		t.Fatalf("RevokeDevice() = %v, %v", revoked, err)
	}
	revokedResponse := httptest.NewRecorder()
	deps.AuthMiddleware()(ok).ServeHTTP(revokedResponse, deviceRequest)
	if revokedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie status = %d", revokedResponse.Code)
	}
}

func TestAdminChallengeIsOneTime(t *testing.T) {
	const secret = "one-time-admin-challenge-secret"
	deps := &Dependencies{
		Config: &config.Config{
			Server: config.ServerConfig{SessionSecret: secret},
			Auth:   config.AuthConfig{Password: "correct password"},
		},
	}
	verifyRequest := httptest.NewRequest(http.MethodPost, "/api/auth/verify", strings.NewReader(`{"password":"correct password"}`))
	verifyRequest.RemoteAddr = "127.0.0.1:12345"
	verifyResponse := httptest.NewRecorder()
	deps.Verify(verifyResponse, verifyRequest)
	if verifyResponse.Code != http.StatusOK {
		t.Fatalf("Verify() status = %d, body = %s", verifyResponse.Code, verifyResponse.Body.String())
	}
	var verified struct {
		ChallengeToken string `json:"challenge_token"`
	}
	if err := json.Unmarshal(verifyResponse.Body.Bytes(), &verified); err != nil || verified.ChallengeToken == "" {
		t.Fatalf("verify response = %s, error = %v", verifyResponse.Body.String(), err)
	}
	body := fmt.Sprintf(`{"challenge_token":%q}`, verified.ChallengeToken)
	login := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		response := httptest.NewRecorder()
		deps.Login(response, request)
		return response
	}
	first := login()
	if first.Code != http.StatusOK || len(first.Result().Cookies()) == 0 {
		t.Fatalf("first Login() status = %d, body = %s", first.Code, first.Body.String())
	}
	second := login()
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("replayed challenge status = %d, want 401", second.Code)
	}
}
