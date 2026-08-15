package handler

import (
	"encoding/json"
	"errors"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityHandlersListAndRead(t *testing.T) {
	activities, _ := store.NewActivityStore(newHandlerStateDB(t))
	activity, _, err := activities.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "研究完成", Content: "结果已准备好。",
		SourceSessionID: "session", SourceMessageID: "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{Activity: activities}

	list := httptest.NewRecorder()
	deps.ListActivities(list, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), activity.ID) {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	read := httptest.NewRecorder()
	deps.MarkActivitiesRead(read, httptest.NewRequest(http.MethodPost, "/api/activity/read", strings.NewReader(`{"ids":["`+activity.ID+`"]}`)))
	if read.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", read.Code, read.Body.String())
	}
	remove := httptest.NewRecorder()
	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/activity/"+activity.ID, nil)
	removeRequest.SetPathValue("id", activity.ID)
	deps.DeleteActivity(remove, removeRequest)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", remove.Code, remove.Body.String())
	}
	if _, err := activities.Get(activity.ID); !errors.Is(err, store.ErrActivityNotFound) {
		t.Fatalf("deleted Activity error = %v", err)
	}
}

func TestAgentProxyResolvesTrustedActivityContextAndCompletesReply(t *testing.T) {
	var received map[string]any
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.URL.Path != "/session/reply-session/prompt_async" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	activities, _ := store.NewActivityStore(newHandlerStateDB(t))
	activity, _, err := activities.Publish(store.ActivityPublish{
		Kind: store.ActivityKindChoice, Title: "选择方向", Content: "请选择后续方向。", Choices: []string{"A", "B"},
		SourceSessionID: "source-session", SourceMessageID: "source-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), nil, nil, nil, activities)
	body := `{"system":"untrusted","marvoContext":{"activity":{"id":"` + activity.ID + `","choices":["A"]}},"parts":[{"type":"text","text":"A"}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/agent/session/reply-session/prompt_async", strings.NewReader(body))
	request.SetPathValue("path", "session/reply-session/prompt_async")
	response := httptest.NewRecorder()
	deps.ProxyJSON(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("reply status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, exists := received["marvoContext"]; exists {
		t.Fatal("Marvo Activity context was forwarded upstream")
	}
	system, _ := received["system"].(string)
	for _, expected := range []string{"source-session", "source-message", "选择方向", `"selected_choices":["A"]`} {
		if !strings.Contains(system, expected) {
			t.Fatalf("trusted Activity system = %q, missing %q", system, expected)
		}
	}
	if strings.Contains(system, "untrusted") {
		t.Fatalf("client system was preserved: %q", system)
	}
	updated, err := activities.Get(activity.ID)
	if err != nil || updated.RespondedAt == nil || updated.ResponseText != "A" || updated.ReplySessionID != "reply-session" {
		t.Fatalf("completed Activity = %#v, %v", updated, err)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/agent/session/other/prompt_async", strings.NewReader(`{"marvoContext":{"activity":{"id":"00000000000000000000000000000000"}},"parts":[{"type":"text","text":"reply"}]}`))
	invalid.SetPathValue("path", "session/other/prompt_async")
	invalidResponse := httptest.NewRecorder()
	deps.ProxyJSON(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || upstreamCalls != 1 {
		t.Fatalf("invalid Activity status = %d, upstream calls = %d", invalidResponse.Code, upstreamCalls)
	}
}

func TestAgentProxyDetachesDeletedActivityReplySession(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/session/reply-session" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	activities, _ := store.NewActivityStore(newHandlerStateDB(t))
	activity, _, err := activities.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "结果", Content: "处理完成。",
		SourceSessionID: "source-session", SourceMessageID: "source-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activities.BeginReply(activity.ID, store.ActivityReply{Text: "收到", SessionID: "reply-session"}); err != nil {
		t.Fatal(err)
	}
	if _, err := activities.CompleteReply(activity.ID, "reply-session"); err != nil {
		t.Fatal(err)
	}

	changed := 0
	deps := NewAgentDeps(upstream.URL, make(chan struct{}), nil, nil, nil, activities)
	deps.SetActivityChangeHandler(func() { changed++ })
	request := httptest.NewRequest(http.MethodDelete, "/api/agent/session/reply-session", nil)
	request.SetPathValue("path", "session/reply-session")
	response := httptest.NewRecorder()
	deps.ProxyJSON(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", response.Code, response.Body.String())
	}
	updated, err := activities.Get(activity.ID)
	if err != nil || updated.ReplySessionID != "" || updated.RespondedAt == nil || updated.ResponseText != "收到" {
		t.Fatalf("deleted reply Activity = %#v, %v", updated, err)
	}
	if changed != 1 {
		t.Fatalf("Activity change notifications = %d", changed)
	}
}
