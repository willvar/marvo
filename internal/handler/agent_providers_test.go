package handler

import (
	"encoding/json"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type providerTestUpstream struct {
	mu           sync.Mutex
	connected    map[string]bool
	integrations map[string]bool
	apiPayload   map[string]any
	callback     map[string]any
	disposed     int
	disconnected string
	removed      []string
}

func newProviderTestUpstream(t *testing.T) (*providerTestUpstream, *httptest.Server) {
	t.Helper()
	state := &providerTestUpstream{
		connected:    map[string]bool{"selected": true, "managed": true, "public": true},
		integrations: map[string]bool{"managed": true},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/provider":
			state.mu.Lock()
			connected := make([]string, 0)
			for id, value := range state.connected {
				if value {
					connected = append(connected, id)
				}
			}
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": connected,
				"all": []any{
					map[string]any{"id": "selected", "name": "Selected", "source": "api", "models": map[string]any{"model": map[string]any{"id": "model"}}},
					map[string]any{"id": "managed", "name": "Managed", "source": "env", "models": map[string]any{}},
					map[string]any{"id": "public", "name": "Public", "source": "custom", "models": map[string]any{"free": map[string]any{"id": "free"}}},
					map[string]any{"id": "key", "name": "Key Provider", "source": "custom", "models": map[string]any{"model": map[string]any{"id": "model"}}},
					map[string]any{"id": "oauth", "name": "OAuth Provider", "source": "custom", "models": map[string]any{"model": map[string]any{"id": "model"}}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/integration":
			if r.URL.Query().Get("location[directory]") != "/workspace" {
				t.Errorf("integration directory = %q", r.URL.Query().Get("location[directory]"))
			}
			state.mu.Lock()
			credential := func(id, connectionType string) []any {
				if !state.integrations[id] {
					return []any{}
				}
				connection := map[string]any{"type": connectionType}
				if connectionType == "credential" {
					connection["id"] = "credential-" + id
					connection["label"] = "default"
				} else {
					connection["name"] = "TEST_" + strings.ToUpper(id) + "_KEY"
				}
				return []any{connection}
			}
			data := []any{
				map[string]any{"id": "selected", "name": "Selected", "methods": []any{map[string]any{"type": "key"}}, "connections": credential("selected", "credential")},
				map[string]any{"id": "managed", "name": "Managed", "methods": []any{map[string]any{"type": "env", "names": []string{"TEST_MANAGED_KEY"}}}, "connections": credential("managed", "env")},
				// Public is usable without a credential, just like OpenCode Zen's free models.
				map[string]any{"id": "public", "name": "Public", "methods": []any{map[string]any{"type": "key"}}, "connections": []any{}},
				map[string]any{"id": "key", "name": "Key Provider", "methods": []any{map[string]any{"type": "key", "label": "Token"}}, "connections": credential("key", "credential")},
				map[string]any{"id": "oauth", "name": "OAuth Provider", "methods": []any{
					map[string]any{"id": "device", "type": "oauth", "label": "Device flow"},
					map[string]any{"id": "code", "type": "oauth", "label": "Code flow"},
				}, "connections": credential("oauth", "credential")},
			}
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"data": data})
		case r.Method == http.MethodGet && r.URL.Path == "/provider/auth":
			writeJSON(w, http.StatusOK, map[string]any{
				"key": []any{map[string]any{
					"type": "api", "label": "Token", "prompts": []any{map[string]any{"type": "text", "key": "tenant", "message": "Tenant"}},
				}},
				"oauth": []any{
					map[string]any{"type": "oauth", "label": "Device flow"},
					map[string]any{"type": "oauth", "label": "Code flow"},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/auth/key":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode API payload: %v", err)
			}
			state.mu.Lock()
			state.apiPayload = payload
			state.connected["key"] = true
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, true)
		case r.Method == http.MethodPost && r.URL.Path == "/provider/oauth/oauth/authorize":
			var payload struct {
				Method int `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode authorize payload: %v", err)
			}
			if payload.Method == 0 {
				writeJSON(w, http.StatusOK, map[string]any{"url": "https://example.com/device", "method": "auto", "instructions": "Enter code: TEST-CODE"})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{"url": "https://example.com/login", "method": "code", "instructions": "Paste the returned code"})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/provider/oauth/oauth/callback":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode callback payload: %v", err)
			}
			state.mu.Lock()
			state.callback = payload
			state.connected["oauth"] = true
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, true)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/auth/"):
			id := strings.TrimPrefix(r.URL.Path, "/auth/")
			state.mu.Lock()
			state.disconnected = id
			state.connected[id] = false
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, true)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/credential/"):
			credentialID := strings.TrimPrefix(r.URL.Path, "/api/credential/")
			id := strings.TrimPrefix(credentialID, "credential-")
			state.mu.Lock()
			state.removed = append(state.removed, credentialID)
			state.integrations[id] = false
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, true)
		case r.Method == http.MethodPost && r.URL.Path == "/instance/dispose":
			if r.URL.Query().Get("directory") != "/workspace" {
				t.Errorf("dispose directory = %q", r.URL.Query().Get("directory"))
			}
			state.mu.Lock()
			state.disposed++
			state.mu.Unlock()
			writeJSON(w, http.StatusOK, true)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	}))
	return state, server
}

func newProviderTestDeps(t *testing.T, upstreamURL string) *AgentDeps {
	t.Helper()
	settingsStore, err := store.NewAgentSettingsStore(newHandlerStateDB(t))
	if err != nil {
		t.Fatal(err)
	}
	globalPromptFile, err := store.NewAgentGlobalPromptFile(filepath.Join(t.TempDir(), "opencode", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	return NewAgentDeps(upstreamURL, make(chan struct{}), settingsStore, nil, globalPromptFile, nil)
}

func TestAgentProviderCatalogAndAPIKeyConnection(t *testing.T) {
	state, upstream := newProviderTestUpstream(t)
	defer upstream.Close()
	deps := newProviderTestDeps(t, upstream.URL)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
	listResponse := httptest.NewRecorder()
	deps.ListProviders(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var catalog struct {
		Providers []agentProviderOption `json:"providers"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	managed := findAgentProvider(catalog.Providers, "managed")
	public := findAgentProvider(catalog.Providers, "public")
	keyProvider := findAgentProvider(catalog.Providers, "key")
	selected := findAgentProvider(catalog.Providers, "selected")
	if managed == nil || !managed.Connected || managed.CanDisconnect || keyProvider == nil || len(keyProvider.Methods) != 1 {
		t.Fatalf("unexpected provider catalog: %#v", catalog.Providers)
	}
	if selected == nil || !selected.Connected || !selected.CanDisconnect {
		t.Fatalf("legacy credential not recognized: %#v", selected)
	}
	if public == nil || public.Connected || public.CanDisconnect {
		t.Fatalf("credential-free public provider reported as connected: %#v", public)
	}
	if strings.Contains(listResponse.Body.String(), "secret") {
		t.Fatal("provider catalog exposed a secret")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/agent/providers/key/connect/key", strings.NewReader(`{"method_index":0,"key":"top-secret","inputs":{"tenant":"acme"}}`))
	request.SetPathValue("providerID", "key")
	response := httptest.NewRecorder()
	deps.ConnectProviderKey(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("connect status = %d, body = %s", response.Code, response.Body.String())
	}
	state.mu.Lock()
	apiPayload := state.apiPayload
	disposed := state.disposed
	state.mu.Unlock()
	if apiPayload["key"] != "top-secret" || disposed != 1 {
		t.Fatalf("API payload = %#v, disposed = %d", apiPayload, disposed)
	}
	metadata, _ := apiPayload["metadata"].(map[string]any)
	if metadata["tenant"] != "acme" {
		t.Fatalf("metadata = %#v", metadata)
	}

	connectedRequest := httptest.NewRequest(http.MethodGet, "/api/agent/providers", nil)
	connectedResponse := httptest.NewRecorder()
	deps.ListProviders(connectedResponse, connectedRequest)
	if connectedResponse.Code != http.StatusOK {
		t.Fatalf("connected list status = %d, body = %s", connectedResponse.Code, connectedResponse.Body.String())
	}
	if err := json.Unmarshal(connectedResponse.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	keyProvider = findAgentProvider(catalog.Providers, "key")
	if keyProvider == nil || !keyProvider.Connected || !keyProvider.CanDisconnect {
		t.Fatalf("saved legacy key not reflected in catalog: %#v", keyProvider)
	}
}

func TestAgentProviderOAuthAutoAndCodeFlows(t *testing.T) {
	state, upstream := newProviderTestUpstream(t)
	defer upstream.Close()
	deps := newProviderTestDeps(t, upstream.URL)

	autoRequest := httptest.NewRequest(http.MethodPost, "/api/agent/providers/oauth/connect/oauth", strings.NewReader(`{"method_index":0,"inputs":{}}`))
	autoRequest.SetPathValue("providerID", "oauth")
	autoResponse := httptest.NewRecorder()
	deps.StartProviderOAuth(autoResponse, autoRequest)
	if autoResponse.Code != http.StatusCreated {
		t.Fatalf("auto start status = %d, body = %s", autoResponse.Code, autoResponse.Body.String())
	}
	var automatic agentProviderOAuthAttemptResponse
	if err := json.Unmarshal(autoResponse.Body.Bytes(), &automatic); err != nil {
		t.Fatal(err)
	}
	if automatic.Mode != "auto" || automatic.Code != "TEST-CODE" {
		t.Fatalf("automatic attempt = %#v", automatic)
	}
	deadline := time.Now().Add(time.Second)
	for automatic.Status == "pending" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		attempt, ok := deps.providerAttempt(automatic.ID)
		if !ok {
			t.Fatal("automatic attempt disappeared")
		}
		automatic = providerAttemptResponse(attempt)
	}
	if automatic.Status != "succeeded" {
		t.Fatalf("automatic attempt status = %q, error = %q", automatic.Status, automatic.Error)
	}

	// Disconnect so the second connection exercises the authorization-code path.
	state.mu.Lock()
	state.connected["oauth"] = false
	state.mu.Unlock()
	codeRequest := httptest.NewRequest(http.MethodPost, "/api/agent/providers/oauth/connect/oauth", strings.NewReader(`{"method_index":1,"inputs":{}}`))
	codeRequest.SetPathValue("providerID", "oauth")
	codeResponse := httptest.NewRecorder()
	deps.StartProviderOAuth(codeResponse, codeRequest)
	if codeResponse.Code != http.StatusCreated {
		t.Fatalf("code start status = %d, body = %s", codeResponse.Code, codeResponse.Body.String())
	}
	var codeAttempt agentProviderOAuthAttemptResponse
	if err := json.Unmarshal(codeResponse.Body.Bytes(), &codeAttempt); err != nil {
		t.Fatal(err)
	}
	completeRequest := httptest.NewRequest(http.MethodPost, "/api/agent/provider-attempts/"+codeAttempt.ID+"/complete", strings.NewReader(`{"code":"returned-code"}`))
	completeRequest.SetPathValue("attemptID", codeAttempt.ID)
	completeResponse := httptest.NewRecorder()
	deps.CompleteProviderOAuth(completeResponse, completeRequest)
	if completeResponse.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body = %s", completeResponse.Code, completeResponse.Body.String())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.callback["code"] != "returned-code" {
		t.Fatalf("callback payload = %#v", state.callback)
	}
}

func TestAgentProviderDisconnectGuardsSelectedAndManagedConnections(t *testing.T) {
	state, upstream := newProviderTestUpstream(t)
	defer upstream.Close()
	deps := newProviderTestDeps(t, upstream.URL)
	if err := deps.settingsStore.Save(store.AgentSettings{Model: &store.AgentModelSelection{ProviderID: "selected", ModelID: "model"}}); err != nil {
		t.Fatal(err)
	}

	for providerID, wantCode := range map[string]int{"selected": http.StatusConflict, "managed": http.StatusConflict} {
		request := httptest.NewRequest(http.MethodDelete, "/api/agent/providers/"+providerID, nil)
		request.SetPathValue("providerID", providerID)
		response := httptest.NewRecorder()
		deps.DisconnectProvider(response, request)
		if response.Code != wantCode {
			t.Fatalf("disconnect %s status = %d, body = %s", providerID, response.Code, response.Body.String())
		}
	}

	state.mu.Lock()
	state.connected["key"] = true
	state.integrations["key"] = true
	state.mu.Unlock()
	request := httptest.NewRequest(http.MethodDelete, "/api/agent/providers/key", nil)
	request.SetPathValue("providerID", "key")
	response := httptest.NewRecorder()
	deps.DisconnectProvider(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("disconnect key status = %d, body = %s", response.Code, response.Body.String())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.disconnected != "key" {
		t.Fatalf("disconnected = %q", state.disconnected)
	}
	if len(state.removed) != 1 || state.removed[0] != "credential-key" {
		t.Fatalf("removed credentials = %#v", state.removed)
	}
}

func TestProviderVerificationCodeRequiresAnExplicitCodeValue(t *testing.T) {
	for input, expected := range map[string]string{
		"Enter code: E2E-CODE":                     "E2E-CODE",
		"Your code is ABCD-1234":                   "ABCD-1234",
		"Paste the authorization code after login": "",
	} {
		if actual := providerVerificationCode(input); actual != expected {
			t.Errorf("providerVerificationCode(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestSanitizedProviderMethodsOmitsUnsupportedBrowserFlow(t *testing.T) {
	methods := []openCodeProviderAuthMethod{
		{Type: "oauth", Label: "ChatGPT Pro/Plus (browser)"},
		{Type: "oauth", Label: "ChatGPT Pro/Plus (headless)"},
		{Type: "key", Label: "Manually enter API Key"},
	}

	got := sanitizedProviderMethods("openai", methods)
	if len(got) != 2 {
		t.Fatalf("methods = %#v, want only supported headless and API key methods", got)
	}
	if got[0].Index != 1 || got[0].Label != "ChatGPT Pro/Plus (headless)" {
		t.Fatalf("first method = %#v", got[0])
	}
	if got[1].Index != 2 || got[1].Label != "Manually enter API Key" {
		t.Fatalf("second method = %#v", got[1])
	}
}
