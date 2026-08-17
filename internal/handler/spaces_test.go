package handler

import (
	"testing"
	"time"

	"marvo/internal/collab"
)

func TestCloseUserEndsLongConnectionBeforeDrainingSpaceLease(t *testing.T) {
	hub := collab.NewHub()
	space := &UserSpace{Hub: hub}
	entry := &cachedUserSpace{
		space: space, leases: 1, lastUsed: time.Now(), closed: make(chan struct{}),
	}
	registry := &SpaceRegistry{
		spaces: make(map[string]*cachedUserSpace), now: time.Now, maxIdle: -1,
	}
	registry.spaces["user"] = entry

	registry.CloseUser("user")
	select {
	case <-hub.Done():
	default:
		t.Fatal("long-lived event hub was not closed")
	}
	if registry.spaces["user"] != entry || !entry.closing {
		t.Fatal("leased space was closed before its users drained")
	}

	registry.release("user", space)
	if registry.spaces["user"] != nil {
		t.Fatal("drained space remained cached")
	}
	select {
	case <-entry.closed:
	default:
		t.Fatal("waiters were not released after the space drained")
	}
}
