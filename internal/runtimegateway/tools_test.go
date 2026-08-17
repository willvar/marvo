package runtimegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"marvo/internal/runtimeevents"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const secondGatewayTestUserID = "a36edc72f3f44fabc012"

type stateRuntimeProvider struct {
	staticRuntimeProvider
	root string
}

func (p *stateRuntimeProvider) StateRoot() string { return p.root }

func TestAgentToolsAuthenticateAndKeepUserStateIsolated(t *testing.T) {
	root := t.TempDir()
	for _, userID := range []string{gatewayTestUserID, secondGatewayTestUserID} {
		if err := os.MkdirAll(filepath.Join(root, "users", userID, "workspace"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	provider := &stateRuntimeProvider{root: root}
	server := NewServer("gateway-secret", provider)
	handler := server.Handler()
	events, unsubscribe := server.events.subscribe()
	defer unsubscribe()

	call := func(userID, token, tool string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/tool/"+userID+"/"+tool, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Marvo-Tool-Token", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	wrongUserToken := agentToolToken("gateway-secret", secondGatewayTestUserID)
	if response := call(gatewayTestUserID, wrongUserToken, "memories", map[string]any{"action": "list"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("cross-user token status = %d", response.Code)
	}
	token := agentToolToken("gateway-secret", gatewayTestUserID)
	added := call(gatewayTestUserID, token, "memories", map[string]any{"action": "add", "text": "默认使用中文。"})
	if added.Code != http.StatusOK {
		t.Fatalf("add memory status = %d, body = %s", added.Code, added.Body.String())
	}
	assertGatewayEvent(t, events, gatewayTestUserID, runtimeevents.KindMemories)
	activity := call(gatewayTestUserID, token, "activity", map[string]any{
		"kind": "choice", "title": "请选择", "content": "选择后我会继续。", "choices": []string{"A", "B"}, "multiple": true,
		"source_session_id": "session", "source_message_id": "message",
	})
	var publishResult map[string]any
	if err := json.Unmarshal(activity.Body.Bytes(), &publishResult); err != nil {
		t.Fatalf("decode Activity result: %v", err)
	}
	if activity.Code != http.StatusOK || len(publishResult) != 1 || publishResult["published"] != true {
		t.Fatalf("publish Activity status = %d, body = %s", activity.Code, activity.Body.String())
	}
	assertGatewayEvent(t, events, gatewayTestUserID, runtimeevents.KindActivity)
	if response := call(gatewayTestUserID, token, "space", map[string]any{"action": "set_brand", "name": "码窝"}); response.Code != http.StatusOK {
		t.Fatalf("set brand status = %d, body = %s", response.Code, response.Body.String())
	}
	assertGatewayEvent(t, events, gatewayTestUserID, runtimeevents.KindSpace)
	scheduledAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	createdTask := call(gatewayTestUserID, token, "schedules", map[string]any{
		"action": "create", "name": "继续研究", "instruction": "检查是否有新资料。",
		"schedule": map[string]any{"kind": "at", "spec": map[string]any{"at": scheduledAt}},
	})
	if createdTask.Code != http.StatusOK {
		t.Fatalf("create schedule status = %d, body = %s", createdTask.Code, createdTask.Body.String())
	}
	assertGatewayEvent(t, events, gatewayTestUserID, runtimeevents.KindSchedules)

	stateA, err := store.OpenStateDB(filepath.Join(root, "users", gatewayTestUserID, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer stateA.Close()
	memoriesA, _ := store.NewMemoryStore(stateA)
	snapshotA, _ := memoriesA.Get()
	if len(snapshotA.Memories) != 1 {
		t.Fatalf("user A memories = %#v", snapshotA.Memories)
	}
	schedulesA, _ := store.NewScheduleStore(stateA)
	tasksA, err := schedulesA.List(context.Background())
	if err != nil || len(tasksA) != 1 || tasksA[0].Name != "继续研究" {
		t.Fatalf("user A schedules = %#v, %v", tasksA, err)
	}
	stateB, err := store.OpenStateDB(filepath.Join(root, "users", secondGatewayTestUserID, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer stateB.Close()
	memoriesB, _ := store.NewMemoryStore(stateB)
	snapshotB, _ := memoriesB.Get()
	if len(snapshotB.Memories) != 0 {
		t.Fatalf("user B received user A memories: %#v", snapshotB.Memories)
	}
	schedulesB, _ := store.NewScheduleStore(stateB)
	tasksB, err := schedulesB.List(context.Background())
	if err != nil || len(tasksB) != 0 {
		t.Fatalf("user B received user A schedules: %#v, %v", tasksB, err)
	}
}

func assertGatewayEvent(t *testing.T, events <-chan runtimeevents.Event, userID string, kind runtimeevents.Kind) {
	t.Helper()
	select {
	case event := <-events:
		if event.UserID != userID || event.Kind != kind {
			t.Fatalf("event = %#v, want user %q kind %q", event, userID, kind)
		}
	case <-time.After(time.Second):
		t.Fatalf("missing event for user %q kind %q", userID, kind)
	}
}

func TestAgentToolsRejectUnknownFieldsAndInvalidOperations(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "users", gatewayTestUserID, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	handler := NewServer("gateway-secret", &stateRuntimeProvider{root: root}).Handler()
	token := agentToolToken("gateway-secret", gatewayTestUserID)
	for _, payload := range []string{
		`{"action":"list","unexpected":true}`,
		`{"action":"unsupported"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/tool/"+gatewayTestUserID+"/memories", bytes.NewBufferString(payload))
		request.Header.Set("X-Marvo-Tool-Token", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("payload %s status = %d, body = %s", payload, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/tool/"+gatewayTestUserID+"/schedules", bytes.NewBufferString(
		`{"action":"next_check","id":"11111111111111111111111111111111","next_check_seconds":9223372036854775807}`,
	))
	request.Header.Set("X-Marvo-Tool-Token", token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized next check status = %d, body = %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/tool/"+gatewayTestUserID+"/schedules", bytes.NewBufferString(
		`{"action":"remove","id":"11111111111111111111111111111111"}`,
	))
	request.Header.Set("X-Marvo-Tool-Token", token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unversioned remove status = %d, body = %s", response.Code, response.Body.String())
	}
}
