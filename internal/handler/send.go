package handler

import (
	"encoding/json"
	"net/http"
)

const maxControlMessageBytes = 64 << 10

func (d *Dependencies) HandleSend(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxControlMessageBytes)
	var msg clientMessage
	if err := readJSON(r, &msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}

	if msg.ClientID == "" {
		clientID := d.Hub.RegisterPoll()
		if clientID == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "server shutting down"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"action": "connected", "client_id": clientID})
		return
	}
	if !d.Hub.PollClientExists(msg.ClientID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "client not found"})
		return
	}

	d.processMessage(msg, msg.ClientID, func(data []byte) {
		d.Hub.SendToPollClient(msg.ClientID, data)
	})
	if msg.Action == "close" {
		d.Hub.UnregisterPoll(msg.ClientID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type clientMessage struct {
	ClientID string          `json:"client_id"`
	Action   string          `json:"action"`
	Title    string          `json:"title"`
	Data     json.RawMessage `json:"data,omitempty"`
}

func (d *Dependencies) processMessage(msg clientMessage, clientID string, respond func([]byte)) {
	switch msg.Action {
	case "subscribe":
		if msg.Title == "" {
			respond(mustJSON(map[string]any{"action": "error", "error": "missing title"}))
			return
		}
		snapshot, err := d.NoteStore.Snapshot(msg.Title)
		if err != nil {
			respond(mustJSON(map[string]any{"action": "error", "error": "note not found", "title": msg.Title}))
			return
		}
		d.Hub.PollSubscribe(clientID, msg.Title)
		respond(mustJSON(map[string]any{
			"action":           "subscribed",
			"title":            msg.Title,
			"subscribers":      d.Hub.GetNoteSubscribers(msg.Title),
			"note":             snapshot.Note,
			"content":          snapshot.Content,
			"content_revision": snapshot.ContentRevision,
			"meta_revision":    snapshot.MetaRevision,
			"instance_token":   snapshot.InstanceToken,
		}))
	case "unsubscribe":
		d.Hub.PollUnsubscribe(clientID, msg.Title)
	case "close":
		// Unregistration is handled after this method returns.
	default:
		respond(mustJSON(map[string]any{"action": "error", "error": "unsupported action"}))
	}
}
