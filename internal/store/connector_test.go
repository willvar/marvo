package store

import (
	"bytes"
	"os"
	"testing"
	"time"
)

const connectorTestSecret = "connector-test-secret-that-is-long-enough-for-key-derivation"

func TestConnectorStoreEncryptsConfigAndActivityPublishCreatesOutbox(t *testing.T) {
	state, _ := newTestStateDB(t)
	connectors, err := NewConnectorStore(state, "user-a", connectorTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	credential := "plaintext-secret-must-not-appear"
	connector, err := connectors.Create("webhook", "自动化", true, map[string]any{
		"url": "https://example.test/hook", "token": credential,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := connectors.Get(connector.ID)
	if err != nil || loaded.Config["token"] != credential {
		t.Fatalf("loaded connector = %#v, %v", loaded, err)
	}

	activities, _ := NewActivityStore(state)
	activity, created, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "完成", Content: "研究已完成。",
		SourceSessionID: "session", SourceMessageID: "message",
	})
	if err != nil || !created {
		t.Fatalf("Publish() = %#v, %t, %v", activity, created, err)
	}
	if _, duplicate, err := activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "完成", Content: "研究已完成。",
		SourceSessionID: "session", SourceMessageID: "message",
	}); err != nil || duplicate {
		t.Fatalf("duplicate publish = %t, %v", duplicate, err)
	}
	var deliveryCount int
	if err := state.sql.QueryRow(`SELECT COUNT(*) FROM activity_deliveries WHERE activity_id = ?`, activity.ID).Scan(&deliveryCount); err != nil || deliveryCount != 1 {
		t.Fatalf("delivery count = %d, %v", deliveryCount, err)
	}

	if _, err := state.sql.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{state.Path(), state.Path() + "-wal"} {
		data, err := os.ReadFile(path)
		if err != nil && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(credential)) {
			t.Fatalf("connector credential appears in %s", path)
		}
	}
}

func TestConnectorDeliveryLeaseRetryAndDisable(t *testing.T) {
	state, _ := newTestStateDB(t)
	connectors, _ := NewConnectorStore(state, "user-a", connectorTestSecret)
	connector, err := connectors.Create("webhook", "Webhook", true, map[string]any{"url": "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	activities, _ := NewActivityStore(state)
	_, _, err = activities.Publish(ActivityPublish{
		Kind: ActivityKindNotice, Title: "通知", Content: "内容", SourceSessionID: "s", SourceMessageID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := connectors.ClaimDue(now, time.Minute)
	if err != nil || claimed == nil || claimed.Attempt != 1 || claimed.Connector.ID != connector.ID {
		t.Fatalf("ClaimDue() = %#v, %v", claimed, err)
	}
	nextAttempt := now.Add(30 * time.Second)
	if err := connectors.MarkFailed(claimed.ID, now, nextAttempt, false, "temporary"); err != nil {
		t.Fatal(err)
	}
	if second, err := connectors.ClaimDue(now.Add(time.Second), time.Minute); err != nil || second != nil {
		t.Fatalf("early claim = %#v, %v", second, err)
	}
	claimed, err = connectors.ClaimDue(nextAttempt, time.Minute)
	if err != nil || claimed == nil || claimed.Attempt != 2 {
		t.Fatalf("retry claim = %#v, %v", claimed, err)
	}
	if _, err := connectors.Update(connector.ID, connector.Name, false, connector.Config); err != nil {
		t.Fatal(err)
	}
	summary, err := connectors.Summary(connector.ID)
	if err != nil || summary.Pending != 0 || summary.Sent != 0 || summary.Failed != 0 {
		t.Fatalf("disabled summary = %#v, %v", summary, err)
	}
}
