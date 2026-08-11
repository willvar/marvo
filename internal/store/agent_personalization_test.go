package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentPersonalizationStorePersistsRulesAndDetectsConflicts(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewAgentPersonalizationStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Rules) != 0 || empty.Revision == "" {
		t.Fatalf("empty snapshot = %#v", empty)
	}

	saved, err := store.Save(empty.Revision, []PersonalizationRule{{Text: "统一使用“智能体”这一称呼。"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Rules) != 1 || !personalizationRuleID.MatchString(saved.Rules[0].ID) || saved.Revision == empty.Revision {
		t.Fatalf("saved snapshot = %#v", saved)
	}
	if _, err := store.Save(empty.Revision, nil); !errors.Is(err, ErrPersonalizationChanged) {
		t.Fatalf("stale save error = %v", err)
	}

	reloaded, err := NewAgentPersonalizationStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reloaded.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != saved.Revision || len(snapshot.Rules) != 1 || snapshot.Rules[0] != saved.Rules[0] {
		t.Fatalf("reloaded snapshot = %#v", snapshot)
	}
}

func TestAgentPersonalizationStoreRejectsInvalidFilesAndRules(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, agentPersonalizationFilename)
	if err := os.WriteFile(path, []byte(`{"rules":[],"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentPersonalizationStore(dataDir); !errors.Is(err, ErrInvalidPersonalization) {
		t.Fatalf("unknown field error = %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	store, err := NewAgentPersonalizationStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	for _, rules := range [][]PersonalizationRule{
		{{ID: "not-a-uuid", Text: "有效内容"}},
		{{Text: ""}},
		{{Text: "重复"}, {Text: "重复"}},
	} {
		if _, err := store.Save(snapshot.Revision, rules); !errors.Is(err, ErrInvalidPersonalization) {
			t.Fatalf("Save(%#v) error = %v", rules, err)
		}
	}
}
