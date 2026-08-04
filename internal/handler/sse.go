package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

func (d *Dependencies) HandleSSE(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")

	isNew := false
	if clientID == "" {
		clientID = d.Hub.RegisterPoll()
		if clientID == "" {
			writeJSON(w, 503, map[string]any{"error": "server shutting down"})
			return
		}
		isNew = true
		slog.Info("sse client registered", "client_id", clientID)
	}

	if !isNew && !d.Hub.PollClientExists(clientID) {
		slog.Info("sse client stale, re-registering", "client_id", clientID)
		if !d.Hub.RegisterPollWithID(clientID) {
			writeJSON(w, 503, map[string]any{"error": "server shutting down"})
			return
		}
		isNew = true
	}
	defer d.Hub.UnregisterPoll(clientID)

	notifyCh := d.Hub.PollNotifyChan(clientID)
	if notifyCh == nil {
		writeJSON(w, 404, map[string]any{"error": "client not found"})
		return
	}

	lastEventID, _ := strconv.ParseInt(r.Header.Get("Last-Event-Id"), 10, 64)

	slog.Info("sse stream started", "client_id", clientID, "last_event_id", lastEventID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	first := mustJSON(map[string]interface{}{
		"action":    "connected",
		"client_id": clientID,
	})
	fmt.Fprintf(w, "id: 0\ndata: %s\n\n", string(first))

	if lastEventID > 0 {
		msgs := d.Hub.PollReplaySince(clientID, lastEventID)
		for _, m := range msgs {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", m.Seq, string(m.Payload))
			lastEventID = m.Seq
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-notifyCh:
			msgs := d.Hub.PollReplaySince(clientID, lastEventID)
			for _, m := range msgs {
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", m.Seq, string(m.Payload))
				lastEventID = m.Seq
			}
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case <-d.Hub.Done():
			return
		case <-r.Context().Done():
			slog.Info("sse client disconnected", "client_id", clientID)
			return
		}
	}
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
