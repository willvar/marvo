package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"marvo/internal/ws"

	"github.com/gofiber/websocket/v2"
)

func (d *Dependencies) HandleWebSocket(c *websocket.Conn) {
	if !d.Hub.BeginHandler() {
		_ = c.Close()
		return
	}
	defer d.Hub.EndHandler()

	clientID := c.Query("client_id")
	if clientID == "" {
		b := make([]byte, 16)
		rand.Read(b)
		clientID = hex.EncodeToString(b)
	}

	client := &ws.Client{
		ID:   clientID,
		Conn: c,
		Send: make(chan []byte, 256),
	}

	if !d.Hub.Register(client) {
		_ = c.Close()
		return
	}
	defer d.Hub.Unregister(client.ID)

	go writePump(client)

	client.Send <- mustJSON(map[string]string{
		"action":    "connected",
		"client_id": clientID,
	})

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}

		var message struct {
			Action   string          `json:"action"`
			Title    string          `json:"title"`
			ClientID string          `json:"client_id"`
			Data     json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg, &message); err != nil {
			continue
		}

		switch message.Action {
		case "subscribe":
			if message.Title != "" {
				_, content, err := d.NoteStore.Get(message.Title)
				if err != nil {
					client.Send <- mustJSON(map[string]interface{}{
						"action": "error",
						"error":  "note not found",
					})
					continue
				}

				doc := d.Hub.OT.InitDocument(message.Title, content)
				d.Hub.Subscribe(client.ID, message.Title)
				count := d.Hub.GetNoteSubscribers(message.Title)
				client.Send <- mustJSON(map[string]interface{}{
					"action":      "subscribed",
					"title":       message.Title,
					"subscribers": count,
					"content":     doc.Content,
					"version":     doc.Version,
				})
			}
		case "ot_steps":
			if message.Title != "" {
				var stepData struct {
					Version int64             `json:"version"`
					Steps   []json.RawMessage `json:"steps"`
					Content string            `json:"content"`
				}
				if err := json.Unmarshal(message.Data, &stepData); err != nil {
					continue
				}

				accepted, missing, newVersion, ok, err := d.Hub.OT.ApplySteps(
					message.Title,
					client.ID,
					stepData.Version,
					stepData.Steps,
					stepData.Content,
				)
				if err != nil {
					client.Send <- mustJSON(map[string]interface{}{
						"action":  "ot_reset_required",
						"title":   message.Title,
						"version": newVersion,
					})
					continue
				}
				if !ok {
					steps, clientIDs := stepPayload(missing)
					client.Send <- mustJSON(map[string]interface{}{
						"action":      "ot_rebase",
						"title":       message.Title,
						"version":     stepData.Version,
						"steps":       steps,
						"client_ids":  clientIDs,
						"new_version": newVersion,
					})
					continue
				}

				if err := d.NoteStore.UpdateContent(message.Title, stepData.Content); err != nil {
					slog.Error("failed to save synced content", "error", err)
					continue
				}

				d.Search.IndexAsync(message.Title, stepData.Content, func(err error) {
					slog.Error("failed to update search index", "error", err)
				})

				steps, clientIDs := stepPayload(accepted)
				broadcast := map[string]interface{}{
					"action":      "ot_steps",
					"title":       message.Title,
					"version":     stepData.Version,
					"steps":       steps,
					"client_ids":  clientIDs,
					"new_version": newVersion,
					"content":     stepData.Content,
				}
				d.Hub.BroadcastToNote(message.Title, "", mustJSON(broadcast))
			}
		}
	}
}

func stepPayload(records []ws.StepRecord) ([]json.RawMessage, []string) {
	steps := make([]json.RawMessage, 0, len(records))
	clientIDs := make([]string, 0, len(records))
	for _, record := range records {
		steps = append(steps, record.Step)
		clientIDs = append(clientIDs, record.ClientID)
	}
	return steps, clientIDs
}

func writePump(client *ws.Client) {
	for msg := range client.Send {
		if err := client.Conn.WriteMessage(1, msg); err != nil {
			break
		}
	}
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
