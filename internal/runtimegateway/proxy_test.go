package runtimegateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const gatewayTestUserID = "f20ac70d-6a6a-4b3c-9e1e-684ee832ea43"

type staticRuntimeProvider struct {
	target *RuntimeTarget
	userID string
	calls  int
}

func (p *staticRuntimeProvider) Ensure(_ context.Context, userID string) (*RuntimeTarget, error) {
	p.calls++
	p.userID = userID
	return p.target, nil
}

func TestGatewayAuthenticatesAndRewritesRuntimeRequest(t *testing.T) {
	var upstreamPath string
	var upstreamQuery string
	var upstreamAuthorization string
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamQuery = r.URL.RawQuery
		upstreamAuthorization = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		upstreamBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	targetURL, _ := url.Parse(upstream.URL)
	provider := &staticRuntimeProvider{target: &RuntimeTarget{URL: targetURL, Username: "opencode", Password: "runtime-password"}}
	handler := NewServer("gateway-secret", provider).Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/user/"+gatewayTestUserID+"/session", nil))
	if unauthorized.Code != http.StatusUnauthorized || provider.calls != 0 {
		t.Fatalf("unauthorized status = %d, ensure calls = %d", unauthorized.Code, provider.calls)
	}

	request := httptest.NewRequest(http.MethodPost, "/user/"+gatewayTestUserID+"/session/abc/prompt_async?directory=%2Fworkspace", strings.NewReader(`{"parts":[]}`))
	request.Header.Set("Authorization", "Bearer gateway-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, body = %s", response.Code, response.Body.String())
	}
	if provider.calls != 1 || provider.userID != gatewayTestUserID {
		t.Fatalf("runtime provider calls = %d, user = %q", provider.calls, provider.userID)
	}
	if upstreamPath != "/session/abc/prompt_async" || upstreamQuery != "directory=%2Fworkspace" || upstreamBody != `{"parts":[]}` {
		t.Fatalf("upstream path = %q, query = %q, body = %q", upstreamPath, upstreamQuery, upstreamBody)
	}
	wantAuthorization := "Basic b3BlbmNvZGU6cnVudGltZS1wYXNzd29yZA=="
	if upstreamAuthorization != wantAuthorization {
		t.Fatalf("upstream authorization = %q", upstreamAuthorization)
	}
}

func TestGatewayRejectsInvalidUserBeforeRuntimeCreation(t *testing.T) {
	target, _ := url.Parse("http://unused.invalid")
	provider := &staticRuntimeProvider{target: &RuntimeTarget{URL: target}}
	handler := NewServer("gateway-secret", provider).Handler()
	request := httptest.NewRequest(http.MethodGet, "/user/not-a-user/session", nil)
	request.Header.Set("Authorization", "Bearer gateway-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("status = %d, ensure calls = %d", response.Code, provider.calls)
	}
}
