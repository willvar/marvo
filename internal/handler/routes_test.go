package handler

import (
	"net/http"
	"testing"

	"marvo/internal/store"
)

func TestRoutesRegistered(t *testing.T) {
	mux := http.NewServeMux()

	deps := &Dependencies{
		AgentDeps:   NewAgentDeps("http://127.0.0.1:4096", make(chan struct{}), nil, nil, nil),
		DeviceStore: store.NewDeviceStore(t.TempDir(), "test-secret"),
	}
	RegisterRoutes(mux, deps)

	hasAuth := false
	hasSSE := false
	hasSend := false

	for _, r := range []struct{ method, path string }{
		{"POST", "/api/auth"},
		{"GET", "/api/events"},
		{"POST", "/api/send"},
		{"GET", "/api/agent/settings"},
		{"PUT", "/api/agent/settings"},
		{"GET", "/api/agent/personalization"},
		{"PUT", "/api/agent/personalization"},
	} {
		req, _ := http.NewRequest(r.method, r.path, nil)
		_, pattern := mux.Handler(req)
		if pattern != r.path && pattern == "" {
			t.Errorf("no handler for %s %s", r.method, r.path)
			continue
		}
		if r.path == "/api/auth" {
			hasAuth = true
		}
		if r.path == "/api/events" {
			hasSSE = true
		}
		if r.path == "/api/send" {
			hasSend = true
		}
	}

	if !hasAuth {
		t.Error("missing POST /api/auth")
	}
	if !hasSSE {
		t.Error("missing GET /api/events")
	}
	if !hasSend {
		t.Error("missing POST /api/send")
	}
}
