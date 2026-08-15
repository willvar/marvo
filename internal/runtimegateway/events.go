package runtimegateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"marvo/internal/runtimeevents"
)

const eventSubscriberBuffer = 128

type eventBroker struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan runtimeevents.Event
}

func newEventBroker() *eventBroker {
	return &eventBroker{subscribers: make(map[uint64]chan runtimeevents.Event)}
}

func (b *eventBroker) subscribe() (<-chan runtimeevents.Event, func()) {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	stream := make(chan runtimeevents.Event, eventSubscriberBuffer)
	b.subscribers[id] = stream
	b.mu.Unlock()

	var once sync.Once
	return stream, func() {
		once.Do(func() {
			b.mu.Lock()
			if current, exists := b.subscribers[id]; exists {
				delete(b.subscribers, id)
				close(current)
			}
			b.mu.Unlock()
		})
	}
}

// publish never lets a slow Marvo process delay a mutating Agent tool call.
// A subscriber that cannot keep up is disconnected; its reconnect resyncs the
// loaded spaces from SQLite, which remains the source of truth.
func (b *eventBroker) publish(event runtimeevents.Event) {
	if !event.Valid() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, stream := range b.subscribers {
		select {
		case stream <- event:
		default:
			delete(b.subscribers, id)
			close(stream)
		}
	}
}

func (s *Server) publishStateEvent(userID string, kind runtimeevents.Kind) {
	s.events.publish(runtimeevents.Event{UserID: userID, Kind: kind})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeGatewayJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	stream, unsubscribe := s.events.subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event, open := <-stream:
			if !open {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: state_changed\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
