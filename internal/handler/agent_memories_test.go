package handler

import (
	"encoding/json"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentMemoriesHandlersSaveMemoriesAndRejectStaleRevision(t *testing.T) {
	memories, err := store.NewMemoryStore(newHandlerStateDB(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps("http://unused.invalid", make(chan struct{}), nil, memories, nil, nil)

	getResponse := httptest.NewRecorder()
	deps.GetMemories(getResponse, httptest.NewRequest(http.MethodGet, "/api/agent/memories", nil))
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var empty memoriesResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Revision == "" || len(empty.Memories) != 0 {
		t.Fatalf("empty response = %#v", empty)
	}

	body := `{"revision":"` + empty.Revision + `","memories":[{"id":"","text":"默认使用“智能体”这一称呼。"}]}`
	putResponse := httptest.NewRecorder()
	deps.UpdateMemories(putResponse, httptest.NewRequest(http.MethodPut, "/api/agent/memories", strings.NewReader(body)))
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", putResponse.Code, putResponse.Body.String())
	}
	var saved memoriesResponse
	if err := json.Unmarshal(putResponse.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Memories) != 1 || saved.Memories[0].ID == "" || saved.Revision == empty.Revision {
		t.Fatalf("saved response = %#v", saved)
	}

	staleResponse := httptest.NewRecorder()
	deps.UpdateMemories(staleResponse, httptest.NewRequest(http.MethodPut, "/api/agent/memories", strings.NewReader(body)))
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status = %d, body = %s", staleResponse.Code, staleResponse.Body.String())
	}
}
