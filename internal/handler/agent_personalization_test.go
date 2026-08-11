package handler

import (
	"encoding/json"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentPersonalizationHandlersSaveRulesAndRejectStaleRevision(t *testing.T) {
	personalization, err := store.NewAgentPersonalizationStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps("http://unused.invalid", make(chan struct{}), nil, personalization, nil)

	getResponse := httptest.NewRecorder()
	deps.GetPersonalization(getResponse, httptest.NewRequest(http.MethodGet, "/api/agent/personalization", nil))
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var empty personalizationResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Revision == "" || len(empty.Rules) != 0 {
		t.Fatalf("empty response = %#v", empty)
	}

	body := `{"revision":"` + empty.Revision + `","rules":[{"id":"","text":"默认使用“智能体”这一称呼。"}]}`
	putResponse := httptest.NewRecorder()
	deps.UpdatePersonalization(putResponse, httptest.NewRequest(http.MethodPut, "/api/agent/personalization", strings.NewReader(body)))
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", putResponse.Code, putResponse.Body.String())
	}
	var saved personalizationResponse
	if err := json.Unmarshal(putResponse.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Rules) != 1 || saved.Rules[0].ID == "" || saved.Revision == empty.Revision {
		t.Fatalf("saved response = %#v", saved)
	}

	staleResponse := httptest.NewRecorder()
	deps.UpdatePersonalization(staleResponse, httptest.NewRequest(http.MethodPut, "/api/agent/personalization", strings.NewReader(body)))
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, body = %s", staleResponse.Code, staleResponse.Body.String())
	}
}
