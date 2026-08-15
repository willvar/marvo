package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"marvo/internal/store"
)

func TestRuntimeEventRefreshesOnlyAnAlreadyLoadedUserSpace(t *testing.T) {
	fixture := newMultiuserFixture(t)
	loadedUser := fixture.createUser(t, "Loaded")
	offlineUser := fixture.createUser(t, "Offline")
	space, release, err := fixture.registry.Acquire(context.Background(), loadedUser.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	clientID := space.Hub.RegisterPoll()
	if clientID == "" {
		t.Fatal("failed to register event observer")
	}
	defer space.Hub.UnregisterPoll(clientID)

	externalState, err := store.OpenStateDB(space.Paths.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	externalBrand, err := store.NewBrandStore(externalState)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := externalBrand.Save("Agent updated brand"); err != nil {
		t.Fatal(err)
	}
	if err := externalState.Close(); err != nil {
		t.Fatal(err)
	}

	stream := "event: state_changed\n" +
		`data: {"user_id":"` + loadedUser.User.ID + `","kind":"space"}` + "\n\n" +
		"event: state_changed\n" +
		`data: {"user_id":"` + offlineUser.User.ID + `","kind":"activity"}` + "\n\n"
	if err := fixture.registry.readRuntimeEventStream(strings.NewReader(stream)); err != nil {
		t.Fatal(err)
	}

	messages := space.Hub.PollReplaySince(clientID, 0)
	if len(messages) != 1 {
		t.Fatalf("loaded-space messages = %d, want 1", len(messages))
	}
	var message struct {
		Action string            `json:"action"`
		Brand  store.BrandConfig `json:"brand"`
	}
	if err := json.Unmarshal(messages[0].Payload, &message); err != nil {
		t.Fatal(err)
	}
	if message.Action != "brand_changed" || message.Brand.Name != "Agent updated brand" {
		t.Fatalf("message = %#v", message)
	}
	fixture.registry.mu.Lock()
	_, offlineWasLoaded := fixture.registry.spaces[offlineUser.User.ID]
	fixture.registry.mu.Unlock()
	if offlineWasLoaded {
		t.Fatal("runtime event initialized an offline user space")
	}
}

func TestRuntimeEventReconnectResyncsLoadedSpaces(t *testing.T) {
	fixture := newMultiuserFixture(t)
	user := fixture.createUser(t, "Resync")
	space, release, err := fixture.registry.Acquire(context.Background(), user.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	clientID := space.Hub.RegisterPoll()
	defer space.Hub.UnregisterPoll(clientID)

	fixture.registry.resyncLoadedSpaces()
	messages := space.Hub.PollReplaySince(clientID, 0)
	wantActions := map[string]bool{
		"activity_changed":       false,
		"brand_changed":          false,
		"agent_memories_changed": false,
		"agent_settings_changed": false,
		"devices_changed":        false,
	}
	for _, message := range messages {
		var payload struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(message.Payload, &payload) == nil {
			if _, exists := wantActions[payload.Action]; exists {
				wantActions[payload.Action] = true
			}
		}
	}
	for action, seen := range wantActions {
		if !seen {
			t.Fatalf("resync did not broadcast %q; messages = %#v", action, messages)
		}
	}
}

func TestSpaceRegistryKeepsLeasedSpacesAndReapsIdleSpaces(t *testing.T) {
	fixture := newMultiuserFixture(t)
	user := fixture.createUser(t, "Lease")
	base := time.Now()
	fixture.registry.now = func() time.Time { return base }
	fixture.registry.idleTTL = time.Minute
	fixture.registry.maxIdle = 32
	space, release, err := fixture.registry.Acquire(context.Background(), user.User.ID)
	if err != nil {
		t.Fatal(err)
	}

	base = base.Add(2 * time.Minute)
	fixture.registry.pruneIdle(true)
	fixture.registry.mu.Lock()
	_, retainedWhileLeased := fixture.registry.spaces[user.User.ID]
	fixture.registry.mu.Unlock()
	if !retainedWhileLeased {
		t.Fatal("leased user space was reaped")
	}

	release()
	base = base.Add(2 * time.Minute)
	fixture.registry.pruneIdle(true)
	fixture.registry.mu.Lock()
	_, retainedAfterIdle := fixture.registry.spaces[user.User.ID]
	fixture.registry.mu.Unlock()
	if retainedAfterIdle {
		t.Fatal("idle user space was not reaped")
	}
	select {
	case <-space.Hub.Done():
	default:
		t.Fatal("reaped user space was not closed")
	}
}

func TestSpaceRegistryBoundsIdleCacheByLastUse(t *testing.T) {
	fixture := newMultiuserFixture(t)
	first := fixture.createUser(t, "First")
	second := fixture.createUser(t, "Second")
	base := time.Now()
	fixture.registry.now = func() time.Time { return base }
	fixture.registry.idleTTL = time.Hour
	fixture.registry.maxIdle = 1

	_, releaseFirst, err := fixture.registry.Acquire(context.Background(), first.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst()
	base = base.Add(time.Minute)
	_, releaseSecond, err := fixture.registry.Acquire(context.Background(), second.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()

	fixture.registry.mu.Lock()
	_, keptFirst := fixture.registry.spaces[first.User.ID]
	_, keptSecond := fixture.registry.spaces[second.User.ID]
	fixture.registry.mu.Unlock()
	if keptFirst || !keptSecond {
		t.Fatalf("idle LRU kept first=%v second=%v", keptFirst, keptSecond)
	}
}
