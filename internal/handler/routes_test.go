package handler

import (
	"context"
	"net/http"
	"testing"

	"marvo/config"
	"marvo/internal/control"
	"marvo/internal/userspace"
)

func TestRoutesExposeOnlyPlatformControlAndUserScopedContent(t *testing.T) {
	stateRoot := t.TempDir()
	layout, err := userspace.OpenLayout(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	controlDB, err := control.Open(layout.ControlDatabase(), multiuserTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer controlDB.Close()
	user, err := controlDB.CreateUser(context.Background(), "Routes", "routes-test-password")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Server:  config.ServerConfig{StateDir: stateRoot, DataDir: t.TempDir(), SessionSecret: multiuserTestSecret},
		Runtime: config.RuntimeConfig{URL: "http://runtime.invalid", Token: "runtime-token"},
	}
	spaces := NewSpaceRegistry(cfg, controlDB, layout, make(chan struct{}))
	defer spaces.Close()
	mux := http.NewServeMux()
	RegisterRoutes(mux, &Dependencies{Config: cfg, Control: controlDB, Layout: layout, Spaces: spaces})

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/health"},
		{http.MethodPost, "/api/platform/auth"},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/user/" + user.User.ID + "/events"},
		{http.MethodPost, "/api/user/" + user.User.ID + "/send"},
		{http.MethodGet, "/api/user/" + user.User.ID + "/agent/settings"},
	} {
		request, _ := http.NewRequest(route.method, route.path, nil)
		if _, pattern := mux.Handler(request); pattern == "" {
			t.Errorf("no handler for %s %s", route.method, route.path)
		}
	}
	for _, legacy := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth"},
		{http.MethodGet, "/api/notes"},
		{http.MethodGet, "/api/events"},
		{http.MethodGet, "/api/agent/settings"},
		{http.MethodGet, "/api/admin/devices"},
	} {
		request, _ := http.NewRequest(legacy.method, legacy.path, nil)
		if _, pattern := mux.Handler(request); pattern != "" {
			t.Errorf("legacy route still registered for %s %s as %q", legacy.method, legacy.path, pattern)
		}
	}
}
