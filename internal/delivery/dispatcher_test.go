package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"marvo/internal/connectors"
	"marvo/internal/control"
	"marvo/internal/store"
	"marvo/internal/userspace"
)

const dispatcherTestSecret = "dispatcher-test-secret-that-is-long-enough-for-encryption"

func TestDispatcherDeliversQueuedActivityAndBuildsPublicLink(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	layout, err := userspace.OpenLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	controlDB, err := control.Open(layout.ControlDatabase(), dispatcherTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer controlDB.Close()
	user, err := controlDB.CreateUser(context.Background(), "Delivery", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.EnsureUser(user.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenStateDB(paths.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	connectorStore, _ := store.NewConnectorStore(state, user.User.ID, dispatcherTestSecret)
	connector, err := connectorStore.Create("webhook", "Test", true, map[string]any{
		"url": server.URL, "method": "POST", "content_type": "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	activities, _ := store.NewActivityStore(state)
	activity, _, err := activities.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "研究完成", Content: "结果已整理。",
		SourceSessionID: "session", SourceMessageID: "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := NewDispatcher(controlDB, layout, dispatcherTestSecret, "https://marvo.example", connectors.NewRegistry(nil))
	dispatcher.Start(ctx)
	dispatcher.Wake(user.User.ID)
	defer func() {
		cancel()
		dispatcher.Wait()
	}()

	select {
	case payload := <-received:
		activityPayload, _ := payload["activity"].(map[string]any)
		if activityPayload["id"] != activity.ID || activityPayload["url"] != "https://marvo.example/user/"+user.User.ID+"/activity?activity="+activity.ID {
			t.Fatalf("webhook Activity = %#v", activityPayload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued Activity was not delivered")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		check, err := store.OpenStateDB(paths.Workspace)
		if err != nil {
			t.Fatal(err)
		}
		checkStore, _ := store.NewConnectorStore(check, user.User.ID, dispatcherTestSecret)
		summary, summaryErr := checkStore.Summary(connector.ID)
		_ = check.Close()
		if summaryErr == nil && summary.Sent == 1 && summary.Pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery summary = %#v, %v", summary, summaryErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
