package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"marvo/config"
)

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
