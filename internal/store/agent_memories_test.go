package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestMemoryStorePersistsMemoriesAndDetectsConflicts(t *testing.T) {
	state, _ := newTestStateDB(t)
	memories, err := NewMemoryStore(state)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := memories.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Memories) != 0 || empty.Revision == "" {
		t.Fatalf("empty snapshot = %#v", empty)
	}

	saved, err := memories.Save(empty.Revision, []Memory{{Text: "统一使用“智能体”这一称呼。"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Memories) != 1 || !memoryIDPattern.MatchString(saved.Memories[0].ID) || saved.Revision == empty.Revision {
		t.Fatalf("saved snapshot = %#v", saved)
	}
	if _, err := memories.Save(empty.Revision, nil); !errors.Is(err, ErrMemoriesChanged) {
		t.Fatalf("stale save error = %v", err)
	}

	reloaded, err := NewMemoryStore(state)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reloaded.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != saved.Revision || len(snapshot.Memories) != 1 || snapshot.Memories[0] != saved.Memories[0] {
		t.Fatalf("reloaded snapshot = %#v", snapshot)
	}
}

func TestMemoryStoreSupportsValidatedAgentOperations(t *testing.T) {
	state, _ := newTestStateDB(t)
	memories, _ := NewMemoryStore(state)
	added, err := memories.Add("默认给出依据。")
	if err != nil || added.ID == "" {
		t.Fatalf("Add() = %#v, %v", added, err)
	}
	duplicate, err := memories.Add("默认给出依据。")
	if err != nil || duplicate != added {
		t.Fatalf("duplicate Add() = %#v, %v", duplicate, err)
	}
	updated, err := memories.Update(added.ID, "默认给出简明依据。")
	if err != nil || updated.Text != "默认给出简明依据。" {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	removed, err := memories.Remove(added.ID)
	if err != nil || !removed {
		t.Fatalf("Remove() = %t, %v", removed, err)
	}
	if removed, err := memories.Remove(added.ID); err != nil || removed {
		t.Fatalf("second Remove() = %t, %v", removed, err)
	}
	for _, values := range [][]Memory{
		{{ID: "not-a-uuid", Text: "有效内容"}},
		{{Text: ""}},
		{{Text: "重复"}, {Text: "重复"}},
	} {
		snapshot, _ := memories.Get()
		if _, err := memories.Save(snapshot.Revision, values); !errors.Is(err, ErrInvalidMemories) {
			t.Fatalf("Save(%#v) error = %v", values, err)
		}
	}
}

func TestMemoryStoreSerializesConcurrentAgentAdds(t *testing.T) {
	workspace := t.TempDir()
	initialized, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := initialized.Close(); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			state, err := OpenStateDB(workspace)
			if err != nil {
				errorsByWorker <- err
				return
			}
			defer state.Close()
			memories, err := NewMemoryStore(state)
			if err == nil {
				_, err = memories.Add(fmt.Sprintf("并发记忆 %d", index))
			}
			if err != nil {
				errorsByWorker <- err
			}
		}(index)
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		t.Errorf("concurrent add: %v", err)
	}

	state, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	memories, _ := NewMemoryStore(state)
	snapshot, err := memories.Get()
	if err != nil || len(snapshot.Memories) != workers {
		t.Fatalf("concurrent memories = %d, error = %v", len(snapshot.Memories), err)
	}
}

func TestMemoryStoreReturnsRevisionConflictForConcurrentAdminSaves(t *testing.T) {
	workspace := t.TempDir()
	firstState, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer firstState.Close()
	secondState, err := OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer secondState.Close()
	first, _ := NewMemoryStore(firstState)
	second, _ := NewMemoryStore(secondState)
	snapshot, err := first.Get()
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for index, memoryStore := range []*MemoryStore{first, second} {
		go func(index int, memoryStore *MemoryStore) {
			<-start
			_, saveErr := memoryStore.Save(snapshot.Revision, []Memory{{Text: fmt.Sprintf("后台记忆 %d", index)}})
			results <- saveErr
		}(index, memoryStore)
	}
	close(start)
	succeeded := 0
	conflicted := 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrMemoriesChanged):
			conflicted++
		default:
			t.Fatalf("concurrent save error = %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent saves: succeeded = %d, conflicted = %d", succeeded, conflicted)
	}
}
