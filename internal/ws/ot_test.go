package ws

import (
	"encoding/json"
	"testing"
)

func TestOTEngineApplyStepsAdvancesVersionAndStoresSnapshot(t *testing.T) {
	engine := NewOTEngine()
	engine.InitDocument("note", "initial")

	steps := []json.RawMessage{rawStep(`{"stepType":"replace","from":1,"to":1}`)}
	accepted, missing, version, ok, err := engine.ApplySteps("note", "client-a", 0, steps, "updated")

	if err != nil {
		t.Fatalf("ApplySteps returned error: %v", err)
	}
	if !ok {
		t.Fatalf("ApplySteps rejected current-version steps")
	}
	if len(missing) != 0 {
		t.Fatalf("expected no missing steps, got %d", len(missing))
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 accepted step, got %d", len(accepted))
	}
	if accepted[0].Version != 1 || accepted[0].ClientID != "client-a" {
		t.Fatalf("unexpected accepted record: %+v", accepted[0])
	}

	doc := engine.GetDocument("note")
	if doc.Version != 1 {
		t.Fatalf("expected stored version 1, got %d", doc.Version)
	}
	if doc.Content != "updated" {
		t.Fatalf("expected stored content %q, got %q", "updated", doc.Content)
	}
	if len(doc.Steps) != 1 {
		t.Fatalf("expected 1 stored step, got %d", len(doc.Steps))
	}
}

func TestOTEngineReturnsMissingStepsForStaleClient(t *testing.T) {
	engine := NewOTEngine()
	engine.InitDocument("note", "initial")

	_, _, _, ok, err := engine.ApplySteps("note", "client-a", 0, []json.RawMessage{rawStep(`{"stepType":"replace","from":1}`)}, "a")
	if err != nil || !ok {
		t.Fatalf("initial ApplySteps failed: ok=%v err=%v", ok, err)
	}
	_, _, _, ok, err = engine.ApplySteps("note", "client-b", 1, []json.RawMessage{rawStep(`{"stepType":"replace","from":2}`)}, "b")
	if err != nil || !ok {
		t.Fatalf("second ApplySteps failed: ok=%v err=%v", ok, err)
	}

	accepted, missing, version, ok, err := engine.ApplySteps("note", "client-c", 0, []json.RawMessage{rawStep(`{"stepType":"replace","from":3}`)}, "c")
	if err != nil {
		t.Fatalf("stale ApplySteps returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected stale ApplySteps to require rebase")
	}
	if len(accepted) != 0 {
		t.Fatalf("expected no accepted steps for stale client, got %d", len(accepted))
	}
	if version != 2 {
		t.Fatalf("expected current version 2, got %d", version)
	}
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing steps, got %d", len(missing))
	}
	if missing[0].Version != 1 || missing[0].ClientID != "client-a" {
		t.Fatalf("unexpected first missing record: %+v", missing[0])
	}
	if missing[1].Version != 2 || missing[1].ClientID != "client-b" {
		t.Fatalf("unexpected second missing record: %+v", missing[1])
	}

	doc := engine.GetDocument("note")
	if doc.Content != "b" {
		t.Fatalf("stale rejected update changed content to %q", doc.Content)
	}
}

func TestOTEngineRejectsFutureVersion(t *testing.T) {
	engine := NewOTEngine()
	engine.InitDocument("note", "initial")

	_, _, version, ok, err := engine.ApplySteps("note", "client-a", 1, []json.RawMessage{rawStep(`{"stepType":"replace"}`)}, "updated")

	if err == nil {
		t.Fatalf("expected error for future version")
	}
	if ok {
		t.Fatalf("expected future version to be rejected")
	}
	if version != 0 {
		t.Fatalf("expected current version 0, got %d", version)
	}
}

func TestOTEngineResetDocumentInvalidatesStepHistory(t *testing.T) {
	engine := NewOTEngine()
	engine.InitDocument("note", "initial")
	_, _, _, ok, err := engine.ApplySteps("note", "client-a", 0, []json.RawMessage{rawStep(`{"stepType":"replace"}`)}, "updated")
	if err != nil || !ok {
		t.Fatalf("ApplySteps failed: ok=%v err=%v", ok, err)
	}

	doc := engine.ResetDocument("note", "external")
	if doc.Version != 2 {
		t.Fatalf("expected reset version 2, got %d", doc.Version)
	}
	if doc.Content != "external" {
		t.Fatalf("expected reset content %q, got %q", "external", doc.Content)
	}
	if len(doc.Steps) != 0 {
		t.Fatalf("expected reset to clear steps, got %d", len(doc.Steps))
	}

	_, _, version, ok, err := engine.ApplySteps("note", "client-b", 1, []json.RawMessage{rawStep(`{"stepType":"replace"}`)}, "stale")
	if err == nil {
		t.Fatalf("expected stale pre-reset client to require snapshot reset")
	}
	if ok {
		t.Fatalf("expected stale pre-reset client to be rejected")
	}
	if version != 2 {
		t.Fatalf("expected current version 2, got %d", version)
	}
}

func TestOTEngineReturnsClones(t *testing.T) {
	engine := NewOTEngine()
	engine.InitDocument("note", "initial")
	_, _, _, ok, err := engine.ApplySteps("note", "client-a", 0, []json.RawMessage{rawStep(`{"stepType":"replace"}`)}, "updated")
	if err != nil || !ok {
		t.Fatalf("ApplySteps failed: ok=%v err=%v", ok, err)
	}

	doc := engine.GetDocument("note")
	doc.Content = "mutated"
	doc.Steps[0].ClientID = "mutated"
	doc.Steps[0].Step[0] = '['

	stored := engine.GetDocument("note")
	if stored.Content != "updated" {
		t.Fatalf("stored content was mutated through clone: %q", stored.Content)
	}
	if stored.Steps[0].ClientID != "client-a" {
		t.Fatalf("stored client id was mutated through clone: %q", stored.Steps[0].ClientID)
	}
	if string(stored.Steps[0].Step) != `{"stepType":"replace"}` {
		t.Fatalf("stored raw step was mutated through clone: %s", stored.Steps[0].Step)
	}
}

func rawStep(s string) json.RawMessage {
	return json.RawMessage(s)
}
