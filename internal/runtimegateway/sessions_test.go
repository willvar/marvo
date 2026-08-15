package runtimegateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type trackedSessionRuntimeProvider struct {
	staticRuntimeProvider
	begins   int
	releases int
}

func (p *trackedSessionRuntimeProvider) BeginUse(userID string) func() {
	p.begins++
	if userID != gatewayTestUserID {
		panic("unexpected user passed to BeginUse")
	}
	return func() { p.releases++ }
}

func TestSessionsToolSearchesOnlyWorkspaceRootSessions(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "opencode" || password != "runtime-password" {
			t.Errorf("upstream Basic Auth = %q/%q, ok = %t", username, password, ok)
		}
		if r.Method != http.MethodGet || r.URL.Path != "/session" {
			t.Errorf("upstream request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query(); got.Get("directory") != "/workspace" || got.Get("scope") != "project" ||
			got.Get("roots") != "true" || got.Get("search") != "研究" || got.Get("limit") != "2" {
			t.Errorf("upstream query = %v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"ses_older","directory":"/workspace","title":"研究旧资料","agent":"secret-agent","time":{"created":1,"updated":10}},
			{"id":"ses_newer","directory":"/workspace","title":"研究新资料","cost":999,"time":{"created":2,"updated":20}},
			{"id":"ses_child","directory":"/workspace","parentID":"ses_newer","title":"研究子任务","time":{"created":3,"updated":30}},
			{"id":"ses_other","directory":"/other","title":"研究别的项目","time":{"created":4,"updated":40}},
			{"id":"../invalid","directory":"/workspace","title":"研究异常会话","time":{"created":5,"updated":45}},
			{"id":"ses_unmatched","directory":"/workspace","title":"无关内容","time":{"created":5,"updated":50}}
		]`))
	}))
	defer upstream.Close()
	targetURL, _ := url.Parse(upstream.URL)
	provider := &trackedSessionRuntimeProvider{staticRuntimeProvider: staticRuntimeProvider{target: &RuntimeTarget{
		URL: targetURL, Username: "opencode", Password: "runtime-password",
	}}}
	handler := NewServer("gateway-secret", provider).Handler()
	response := callSessionsTool(t, handler, agentToolToken("gateway-secret", gatewayTestUserID), map[string]any{
		"action": "search", "query": " 研究 ", "limit": 2,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Sessions []safeSessionSummary `json:"sessions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 2 || result.Sessions[0].ID != "ses_newer" || result.Sessions[1].ID != "ses_older" {
		t.Fatalf("safe sessions = %#v", result.Sessions)
	}
	if body := response.Body.String(); strings.Contains(body, "secret-agent") || strings.Contains(body, "cost") || strings.Contains(body, "ses_child") {
		t.Fatalf("search response leaked internal or non-root data: %s", body)
	}
	if provider.calls != 1 || provider.userID != gatewayTestUserID || provider.begins != 1 || provider.releases != 1 {
		t.Fatalf("provider calls=%d user=%q begin=%d release=%d", provider.calls, provider.userID, provider.begins, provider.releases)
	}
}

func TestSessionsToolReadsOnlySafeConversationContent(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, ok := r.BasicAuth()
		if !ok || username != "opencode" || password != "runtime-password" {
			t.Errorf("upstream Basic Auth = %q/%q, ok = %t", username, password, ok)
		}
		if r.URL.Query().Get("directory") != "/workspace" {
			t.Errorf("upstream directory = %q", r.URL.Query().Get("directory"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/session/ses_safe123":
			_, _ = w.Write([]byte(`{"id":"ses_safe123","directory":"/workspace","title":"安全历史","model":"secret-model","time":{"created":1,"updated":2}}`))
		case "/session/ses_safe123/message":
			if r.URL.Query().Get("limit") != "7" {
				t.Errorf("message limit = %q", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`[
				{"info":{"role":"user","time":{"created":10},"system":"secret-system","model":{"providerID":"secret-provider"}},"parts":[
					{"type":"text","text":"用户问题"},
					{"type":"text","text":"secret-synthetic","synthetic":true},
					{"type":"file","filename":"资料.pdf","url":"data:secret-file"},
					{"type":"reasoning","text":"secret-reasoning"}
				]},
				{"info":{"role":"assistant","time":{"created":20},"providerID":"secret-provider","tokens":{"input":123}},"parts":[
					{"type":"tool","state":{"output":"secret-tool-output"}},
					{"type":"text","text":"普通回答"},
					{"type":"text","text":"secret-ignored","ignored":true}
				]},
				{"info":{"role":"assistant","time":{"created":30}},"parts":[{"type":"tool","state":{"output":"secret-only-tool"}}]},
				{"info":{"role":"system","time":{"created":40}},"parts":[{"type":"text","text":"secret-system-role"}]}
			]`))
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	targetURL, _ := url.Parse(upstream.URL)
	provider := &staticRuntimeProvider{target: &RuntimeTarget{URL: targetURL, Username: "opencode", Password: "runtime-password"}}
	handler := NewServer("gateway-secret", provider).Handler()
	response := callSessionsTool(t, handler, agentToolToken("gateway-secret", gatewayTestUserID), map[string]any{
		"action": "read", "session_id": "ses_safe123", "limit": 7,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Session struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"session"`
		Messages  []safeSessionMessage `json:"messages"`
		Truncated bool                 `json:"truncated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || result.Session.ID != "ses_safe123" || result.Session.Title != "安全历史" || result.Truncated {
		t.Fatalf("session result = %#v, requests = %d", result, requests)
	}
	if len(result.Messages) != 2 || result.Messages[0].Role != "user" || result.Messages[0].Text != "用户问题" ||
		len(result.Messages[0].Attachments) != 1 || result.Messages[0].Attachments[0] != "资料.pdf" ||
		result.Messages[1].Role != "assistant" || result.Messages[1].Text != "普通回答" {
		t.Fatalf("safe messages = %#v", result.Messages)
	}
	for _, forbidden := range []string{
		"secret-system", "secret-provider", "secret-file", "secret-reasoning", "secret-tool", "secret-ignored", "tokens", "providerID",
	} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("read response contains %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestSessionsToolRejectsInvalidInputBeforeStartingRuntime(t *testing.T) {
	provider := &staticRuntimeProvider{}
	handler := NewServer("gateway-secret", provider).Handler()
	token := agentToolToken("gateway-secret", gatewayTestUserID)
	for _, payload := range []string{
		`{"action":"read","session_id":"../secret"}`,
		`{"action":"read","session_id":"ses_valid","query":"unexpected"}`,
		`{"action":"read","session_id":"ses_valid","limit":101}`,
		`{"action":"search","session_id":"ses_valid"}`,
		`{"action":"search","limit":101}`,
		`{"action":"search","unexpected":true}`,
		`{"action":"delete","session_id":"ses_valid"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/tool/"+gatewayTestUserID+"/sessions", bytes.NewBufferString(payload))
		request.Header.Set("X-Marvo-Tool-Token", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("payload %s status = %d, body = %s", payload, response.Code, response.Body.String())
		}
	}
	if provider.calls != 0 {
		t.Fatalf("invalid input started runtime %d times", provider.calls)
	}
}

func TestSessionsToolRejectsCrossDirectorySessionAndHidesUpstreamErrors(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/session/ses_other" {
			_, _ = w.Write([]byte(`{"id":"ses_other","directory":"/private","title":"Other"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream-secret"}`))
	}))
	defer upstream.Close()
	targetURL, _ := url.Parse(upstream.URL)
	provider := &staticRuntimeProvider{target: &RuntimeTarget{URL: targetURL}}
	handler := NewServer("gateway-secret", provider).Handler()
	token := agentToolToken("gateway-secret", gatewayTestUserID)

	response := callSessionsTool(t, handler, token, map[string]any{"action": "read", "session_id": "ses_other"})
	if response.Code != http.StatusNotFound || requests != 1 {
		t.Fatalf("cross-directory status=%d requests=%d body=%s", response.Code, requests, response.Body.String())
	}
	response = callSessionsTool(t, handler, token, map[string]any{"action": "search"})
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "upstream-secret") {
		t.Fatalf("upstream error status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSessionMessageFilteringCapsSafeOutput(t *testing.T) {
	message := openCodeSessionMessage{}
	message.Info.Role = "assistant"
	message.Parts = append(message.Parts, struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Filename  string `json:"filename"`
		Synthetic bool   `json:"synthetic"`
		Ignored   bool   `json:"ignored"`
	}{Type: "text", Text: strings.Repeat("字", maxSessionResultText)})
	messages, truncated := filterSessionMessages([]openCodeSessionMessage{message})
	if !truncated || len(messages) != 1 || len(messages[0].Text) > maxSessionPartText || !json.Valid([]byte(`"`+messages[0].Text+`"`)) {
		t.Fatalf("filter result bytes=%d truncated=%t", len(messages[0].Text), truncated)
	}
}

func callSessionsTool(t *testing.T, handler http.Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/tool/"+gatewayTestUserID+"/sessions", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Marvo-Tool-Token", token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
