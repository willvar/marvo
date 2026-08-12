package handler

import (
	"context"
	"encoding/json"
	"errors"
	"marvo/internal/agentcredentials"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestApplyMarvoPromptContext(t *testing.T) {
	body := []byte(`{"system":"client supplied system","marvoContext":{"note":{"title":"测试 note"},"viewport":{"width":1366,"height":768,"devicePixelRatio":1.5}},"parts":[]}`)
	result, title, err := applyMarvoPromptContext("session/ses_1/prompt_async", http.MethodPost, body)
	if err != nil {
		t.Fatal(err)
	}
	if title != "测试 note" {
		t.Fatalf("note title = %q", title)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["marvoContext"]; exists {
		t.Fatal("Marvo context was forwarded upstream")
	}
	system, _ := payload["system"].(string)
	if strings.Contains(system, "client supplied system") || !strings.Contains(system, `"title":"测试 note"`) ||
		!strings.Contains(system, `"devicePixelRatio":1.5`) {
		t.Fatalf("normalized system = %q", system)
	}

	for _, invalid := range []string{
		`{"marvoContext":{"note":{"title":"../secret"}}}`,
		`{"marvoContext":{"viewport":{"width":0,"height":768,"devicePixelRatio":1}}}`,
		`{"marvoContext":{"unexpected":true}}`,
	} {
		if _, _, err := applyMarvoPromptContext("session/ses_1/prompt_async", http.MethodPost, []byte(invalid)); err == nil {
			t.Fatalf("invalid context accepted: %s", invalid)
		}
	}
}

func TestAgentSettingsListsConnectedModelsAndPersistsSelection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{})
		case "/provider":
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": []string{"fake"},
				"all": []any{
					map[string]any{
						"id": "fake", "name": "Fake Provider",
						"models": map[string]any{
							"vision": map[string]any{
								"id": "vision", "providerID": "fake", "name": "Vision Model", "family": "vision", "status": "active",
								"variants": map[string]any{"high": map[string]string{"reasoningEffort": "high"}, "low": map[string]string{"reasoningEffort": "low"}},
								"capabilities": map[string]any{
									"attachment": true, "reasoning": true, "toolcall": true,
									"input":  map[string]bool{"text": true, "image": true, "video": false, "audio": false, "pdf": true},
									"output": map[string]bool{"text": true, "image": false, "video": false, "audio": false, "pdf": false},
								},
								"limit": map[string]int{"context": 128000, "output": 16000},
							},
						},
					},
					map[string]any{
						"id": "offline", "name": "Offline Provider",
						"models": map[string]any{"hidden": map[string]any{"id": "hidden", "name": "Hidden"}},
					},
				},
			})
		case "/config":
			writeJSON(w, http.StatusOK, map[string]string{"model": "fake/vision"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	settingsDirectory := t.TempDir()
	if err := os.Chmod(settingsDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	settingsStore, err := store.NewAgentSettingsStore(settingsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	globalPromptPath := filepath.Join(t.TempDir(), "opencode", "AGENTS.md")
	globalPromptFile, err := store.NewAgentGlobalPromptFile(globalPromptPath)
	if err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), settingsStore, nil, globalPromptFile)
	credentialStore, err := agentcredentials.NewStore(
		settingsDirectory,
		"f20ac70d6a6a4b3c9e1e",
		"handler-agent-credential-test-secret-with-enough-entropy",
	)
	if err != nil {
		t.Fatal(err)
	}
	deps.SetCredentialStore(credentialStore)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/agent/settings", nil)
	getResponse := httptest.NewRecorder()
	deps.GetSettings(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var initial agentSettingsResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Source != "opencode" || initial.Model == nil || initial.Model.ModelID != "vision" || !initial.ModelAvailable || initial.ExaConfigured {
		t.Fatalf("initial settings = %#v", initial)
	}
	if len(initial.Models) != 1 || !initial.Models[0].Capabilities.Input.Image || !initial.Models[0].Capabilities.Input.PDF {
		t.Fatalf("connected models = %#v", initial.Models)
	}
	if got := strings.Join(initial.Models[0].Variants, ","); got != "low,high" {
		t.Fatalf("model variants = %q, want low,high", got)
	}

	updateBody := `{"model":{"provider_id":"fake","model_id":"vision"},"variant":"high","global_prompt":"始终使用中文","exa_api_key":"exa-handler-secret"}`
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/agent/settings", strings.NewReader(updateBody))
	updateResponse := httptest.NewRecorder()
	deps.UpdateSettings(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated agentSettingsResponse
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.GlobalPromptPending {
		t.Fatal("idle global prompt update was left pending")
	}
	if !updated.ExaConfigured || strings.Contains(updateResponse.Body.String(), "exa-handler-secret") {
		t.Fatalf("updated Exa status = %t, response = %s", updated.ExaConfigured, updateResponse.Body.String())
	}
	credentials, err := credentialStore.Load()
	if err != nil || credentials.ExaAPIKey != "exa-handler-secret" {
		t.Fatalf("stored Agent credentials = %#v, error = %v", credentials, err)
	}
	credentialFile, err := os.ReadFile(filepath.Join(settingsDirectory, ".agent-credentials.json"))
	if err != nil || strings.Contains(string(credentialFile), "exa-handler-secret") {
		t.Fatalf("encrypted credential file = %q, error = %v", credentialFile, err)
	}
	globalInstructions, err := os.ReadFile(globalPromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(globalInstructions), "始终使用中文") {
		t.Fatalf("global instructions = %q", globalInstructions)
	}
	stored := settingsStore.Get()
	if stored.Model == nil || stored.Model.ModelID != "vision" || stored.Variant != "high" || stored.GlobalPrompt != "始终使用中文" {
		t.Fatalf("stored settings = %#v", stored)
	}

	clearRequest := httptest.NewRequest(http.MethodPut, "/api/agent/settings", strings.NewReader(`{"model":{"provider_id":"fake","model_id":"vision"},"variant":"high","global_prompt":"始终使用中文","clear_exa_api_key":true}`))
	clearResponse := httptest.NewRecorder()
	deps.UpdateSettings(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusOK {
		t.Fatalf("clear Exa key status = %d, body = %s", clearResponse.Code, clearResponse.Body.String())
	}
	var cleared agentSettingsResponse
	if err := json.Unmarshal(clearResponse.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.ExaConfigured {
		t.Fatal("cleared Exa key is still reported as configured")
	}
	credentials, err = credentialStore.Load()
	if err != nil || credentials.ExaAPIKey != "" {
		t.Fatalf("cleared Agent credentials = %#v, error = %v", credentials, err)
	}

	conflictingCredentialRequest := httptest.NewRequest(http.MethodPut, "/api/agent/settings", strings.NewReader(`{"model":{"provider_id":"fake","model_id":"vision"},"variant":"high","global_prompt":"始终使用中文","exa_api_key":"secret","clear_exa_api_key":true}`))
	conflictingCredentialResponse := httptest.NewRecorder()
	deps.UpdateSettings(conflictingCredentialResponse, conflictingCredentialRequest)
	if conflictingCredentialResponse.Code != http.StatusBadRequest {
		t.Fatalf("conflicting Exa update status = %d, want 400", conflictingCredentialResponse.Code)
	}

	invalidRequest := httptest.NewRequest(http.MethodPut, "/api/agent/settings", strings.NewReader(`{"model":{"provider_id":"offline","model_id":"hidden"},"global_prompt":""}`))
	invalidResponse := httptest.NewRecorder()
	deps.UpdateSettings(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("unavailable model status = %d, want 400", invalidResponse.Code)
	}

	invalidVariantRequest := httptest.NewRequest(http.MethodPut, "/api/agent/settings", strings.NewReader(`{"model":{"provider_id":"fake","model_id":"vision"},"variant":"max","global_prompt":""}`))
	invalidVariantResponse := httptest.NewRecorder()
	deps.UpdateSettings(invalidVariantResponse, invalidVariantRequest)
	if invalidVariantResponse.Code != http.StatusBadRequest {
		t.Fatalf("unsupported variant status = %d, want 400", invalidVariantResponse.Code)
	}
}

func TestAgentProxyInjectsSavedModelAndLoadsGlobalPromptFromOpenCodeRules(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{})
		case "/session/ses_1/prompt_async":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	settingsStore, err := store.NewAgentSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := settingsStore.Save(store.AgentSettings{
		Model:        &store.AgentModelSelection{ProviderID: "chosen-provider", ModelID: "chosen/model"},
		Variant:      "high",
		GlobalPrompt: "Marvo-Note-Title: should-not-lock\n始终使用中文",
	}); err != nil {
		t.Fatal(err)
	}
	globalPromptPath := filepath.Join(t.TempDir(), "opencode", "AGENTS.md")
	globalPromptFile, err := store.NewAgentGlobalPromptFile(globalPromptPath)
	if err != nil {
		t.Fatal(err)
	}
	personalization, err := store.NewAgentPersonalizationStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	personalizationSnapshot, err := personalization.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := personalization.Save(personalizationSnapshot.Revision, []store.PersonalizationRule{{
		Text: "统一使用“智能体”这一称呼。",
	}}); err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), settingsStore, personalization, globalPromptFile)
	body := `{"model":{"providerID":"old-provider","modelID":"old-model"},"variant":"low","system":"client supplied system","marvoContext":{"note":{"title":"actual-note"},"viewport":{"width":1366,"height":768,"devicePixelRatio":1}},"parts":[{"type":"text","text":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/session/ses_1/prompt_async", strings.NewReader(body))
	req.SetPathValue("path", "session/ses_1/prompt_async")
	response := httptest.NewRecorder()
	deps.ProxyJSON(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("prompt status = %d, body = %s", response.Code, response.Body.String())
	}
	model, _ := received["model"].(map[string]any)
	if model["providerID"] != "chosen-provider" || model["modelID"] != "chosen/model" {
		t.Fatalf("injected model = %#v", model)
	}
	if received["variant"] != "high" {
		t.Fatalf("injected variant = %#v, want high", received["variant"])
	}
	system, _ := received["system"].(string)
	if strings.Contains(system, "client supplied system") || !strings.Contains(system, `"title":"actual-note"`) ||
		!strings.Contains(system, `"width":1366`) {
		t.Fatalf("injected system = %q", system)
	}
	globalInstructions, err := os.ReadFile(globalPromptPath)
	if err != nil {
		t.Fatal(err)
	}
	instructions := string(globalInstructions)
	if !strings.Contains(instructions, "始终使用中文") || !strings.Contains(instructions, "统一使用“智能体”") ||
		strings.Index(instructions, "统一使用“智能体”") >= strings.Index(instructions, "始终使用中文") ||
		strings.Contains(system, "始终使用中文") {
		t.Fatalf("global instructions = %q, request system = %q", globalInstructions, system)
	}
	deps.runMu.Lock()
	_, actualLocked := deps.noteRuns["actual-note"]
	_, spoofedLocked := deps.noteRuns["should-not-lock"]
	deps.runMu.Unlock()
	if !actualLocked || spoofedLocked {
		t.Fatalf("note locks: actual=%v spoofed=%v", actualLocked, spoofedLocked)
	}
}

func TestGlobalPromptActivationWaitsForRunningSessions(t *testing.T) {
	statuses := map[string]any{"ses_active": map[string]string{"type": "busy"}}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session/status" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, statuses)
	}))
	defer upstream.Close()

	settingsStore, err := store.NewAgentSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldSettings := store.AgentSettings{GlobalPrompt: "旧偏好"}
	if err := settingsStore.Save(oldSettings); err != nil {
		t.Fatal(err)
	}
	globalPromptFile, err := store.NewAgentGlobalPromptFile(filepath.Join(t.TempDir(), "opencode", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := globalPromptFile.SyncPreferences(oldSettings.GlobalPrompt, nil); err != nil {
		t.Fatal(err)
	}
	if err := settingsStore.Save(store.AgentSettings{GlobalPrompt: "新偏好"}); err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), settingsStore, nil, globalPromptFile)

	pending, err := deps.activateSavedGlobalPrompt(context.Background())
	if err != nil || !pending {
		t.Fatalf("busy activation pending = %v, error = %v", pending, err)
	}
	if matches, err := globalPromptFile.MatchesPreferences(oldSettings.GlobalPrompt, nil); err != nil || !matches {
		t.Fatalf("active prompt changed during a run: match = %v, error = %v", matches, err)
	}
	if err := deps.beginAgentPrompt(context.Background(), "ses_new"); !errors.Is(err, errAgentGlobalPromptPending) {
		t.Fatalf("new prompt while settings pending error = %v", err)
	}

	statuses = map[string]any{}
	pending, err = deps.activateSavedGlobalPrompt(context.Background())
	if err != nil || pending {
		t.Fatalf("idle activation pending = %v, error = %v", pending, err)
	}
	if matches, err := globalPromptFile.MatchesPreferences("新偏好", nil); err != nil || !matches {
		t.Fatalf("new prompt was not activated: match = %v, error = %v", matches, err)
	}
}

func TestAgentProxyCompactsLongContextBeforeAttachmentPrompt(t *testing.T) {
	var calls []string
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /session/ses_long/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{
					"info":  map[string]any{"id": "user_1", "role": "user"},
					"parts": []any{map[string]any{"type": "text", "text": "first"}},
				},
				map[string]any{
					"info": map[string]any{
						"id": "assistant_1", "role": "assistant", "providerID": "fake", "modelID": "vision",
						"tokens": map[string]any{"total": 50_000},
					},
				},
				map[string]any{
					"info":  map[string]any{"id": "user_2", "role": "user"},
					"parts": []any{map[string]any{"type": "text", "text": "second"}},
				},
				map[string]any{
					"info": map[string]any{
						"id": "assistant_2", "role": "assistant", "providerID": "fake", "modelID": "vision",
						"tokens": map[string]any{"total": 70_000},
					},
				},
			})
		case "GET /provider":
			writeJSON(w, http.StatusOK, map[string]any{
				"connected": []string{"fake"},
				"all": []any{map[string]any{
					"id": "fake",
					"models": map[string]any{"vision": map[string]any{
						"id": "vision", "providerID": "fake", "name": "Vision",
						"limit": map[string]int64{"context": 128_000, "output": 16_000},
					}},
				}},
			})
		case "POST /api/session/ses_long/compact", "POST /api/session/ses_long/wait":
			w.WriteHeader(http.StatusNoContent)
		case "POST /session/ses_long/prompt_async":
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	settingsStore, err := store.NewAgentSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := settingsStore.Save(store.AgentSettings{
		Model: &store.AgentModelSelection{ProviderID: "fake", ModelID: "vision"},
	}); err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), settingsStore, nil, nil)
	body := `{"parts":[{"type":"text","text":"inspect"},{"type":"file","mime":"image/png","filename":"photo.png","url":"data:image/png;base64,AA=="}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/session/ses_long/prompt_async", strings.NewReader(body))
	req.SetPathValue("path", "session/ses_long/prompt_async")
	response := httptest.NewRecorder()
	deps.ProxyJSON(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("attachment prompt status = %d, body = %s", response.Code, response.Body.String())
	}
	wantCalls := []string{
		"GET /session/ses_long/message",
		"GET /provider",
		"POST /api/session/ses_long/compact",
		"POST /api/session/ses_long/wait",
		"POST /session/ses_long/prompt_async",
	}
	if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("OpenCode calls = %#v, want %#v", calls, wantCalls)
	}
	parts, _ := received["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("forwarded prompt parts = %#v", received["parts"])
	}
}

func TestAgentProxyInjectsOpenCodeModelBeforeSettingsAreSaved(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config":
			writeJSON(w, http.StatusOK, map[string]string{"model": "fallback-provider/fallback/model"})
		case "/session/ses_1/prompt_async":
			_ = json.NewDecoder(r.Body).Decode(&received)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	settingsStore, err := store.NewAgentSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), settingsStore, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/session/ses_1/prompt_async", strings.NewReader(`{"variant":"client-value","parts":[{"type":"text","text":"hello"}]}`))
	req.SetPathValue("path", "session/ses_1/prompt_async")
	response := httptest.NewRecorder()
	deps.ProxyJSON(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("prompt status = %d, body = %s", response.Code, response.Body.String())
	}
	model, _ := received["model"].(map[string]any)
	if model["providerID"] != "fallback-provider" || model["modelID"] != "fallback/model" {
		t.Fatalf("fallback model = %#v", model)
	}
	if _, exists := received["variant"]; exists {
		t.Fatalf("client variant was not removed: %#v", received["variant"])
	}
}

func TestAgentProxyAllowsDifferentNotesButLocksSameNote(t *testing.T) {
	var mu sync.Mutex
	statuses := map[string]map[string]string{}
	called := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/status" {
			mu.Lock()
			defer mu.Unlock()
			_ = json.NewEncoder(w).Encode(statuses)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 3 && parts[0] == "session" && parts[2] == "prompt_async" {
			mu.Lock()
			called[parts[1]]++
			statuses[parts[1]] = map[string]string{"type": "busy"}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	deps := NewAgentDeps(upstream.URL, make(chan struct{}), nil, nil, nil)
	request := func(sessionID, title string) *httptest.ResponseRecorder {
		body := `{"marvoContext":{"note":{"title":"` + title + `"}},"parts":[{"type":"text","text":"test"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/agent/session/"+sessionID+"/prompt_async", strings.NewReader(body))
		req.SetPathValue("path", "session/"+sessionID+"/prompt_async")
		recorder := httptest.NewRecorder()
		deps.ProxyJSON(recorder, req)
		return recorder
	}

	if response := request("one", "note-a"); response.Code != http.StatusNoContent {
		t.Fatalf("first prompt status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := request("two", "note-a"); response.Code != http.StatusConflict {
		t.Fatalf("same-note prompt status = %d, want 409", response.Code)
	}
	if response := request("two", "note-b"); response.Code != http.StatusNoContent {
		t.Fatalf("different-note prompt status = %d, body = %s", response.Code, response.Body.String())
	}

	mu.Lock()
	if called["one"] != 1 || called["two"] != 1 {
		mu.Unlock()
		t.Fatalf("upstream calls = %#v", called)
	}
	mu.Unlock()

	deps.runMu.Lock()
	_, reserved := deps.noteRuns["note-a"]
	deps.runMu.Unlock()
	if !reserved {
		t.Fatal("accepted note task did not retain its Agent concurrency reservation")
	}
	mu.Lock()
	statuses["one"] = map[string]string{"type": "idle"}
	mu.Unlock()
	deps.runMu.Lock()
	run := deps.noteRuns["note-a"]
	run.Reserved = time.Now().Add(-6 * time.Second)
	deps.noteRuns["note-a"] = run
	deps.runMu.Unlock()
	deps.refreshNoteRuns(context.Background())
	deps.runMu.Lock()
	_, reserved = deps.noteRuns["note-a"]
	deps.runMu.Unlock()
	if reserved {
		t.Fatal("idle Agent task retained its concurrency reservation")
	}
}

func TestAgentShareRoutesAreRemovedWithoutCallingOpenCode(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), nil, nil, nil)
	for _, path := range []string{"session/ses_1/share", "session/ses_1/unshare"} {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/"+path, nil)
		req.SetPathValue("path", path)
		response := httptest.NewRecorder()
		deps.ProxyJSON(response, req)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, response.Code)
		}
	}
	if called {
		t.Fatal("removed Agent share route reached OpenCode")
	}
}
