package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

type Client struct {
	ID        string
	Conn      *websocket.Conn
	NoteTitle string
	Send      chan []byte
}

type Message struct {
	Action string      `json:"action"`
	Data   interface{} `json:"data"`
}

type Hub struct {
	mu        sync.RWMutex
	clients   map[string]*Client
	noteSubs  map[string]map[string]*Client
	OT        *OTEngine
	done      chan struct{}
	closing   bool
	closeOnce sync.Once
	handlers  sync.WaitGroup
}

func NewHub() *Hub {
	return &Hub{
		clients:  make(map[string]*Client),
		noteSubs: make(map[string]map[string]*Client),
		OT:       NewOTEngine(),
		done:     make(chan struct{}),
	}
}

func (h *Hub) Run() {
	slog.Info("websocket hub started")
	go h.heartbeat()
}

func (h *Hub) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.RLock()
			if h.closing {
				h.mu.RUnlock()
				return
			}
			for id, client := range h.clients {
				select {
				case client.Send <- mustRaw(map[string]string{"action": "heartbeat"}):
				default:
					slog.Warn("ws client unreachable", "client", id)
					go func(cid string) { h.Unregister(cid) }(id)
				}
			}
			h.mu.RUnlock()
		case <-h.done:
			return
		}
	}
}

func (h *Hub) BeginHandler() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.handlers.Add(1)
	return true
}

func (h *Hub) EndHandler() {
	h.handlers.Done()
}

func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closing = true
		clients := make([]*Client, 0, len(h.clients))
		for _, client := range h.clients {
			clients = append(clients, client)
		}
		h.mu.Unlock()

		close(h.done)
		for _, client := range clients {
			_ = client.Conn.Close()
		}
	})
}

func mustRaw(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

func (h *Hub) Register(client *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.clients[client.ID] = client
	return true
}

func (h *Hub) SendToClient(clientID string, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return
	}
	if client, ok := h.clients[clientID]; ok {
		select {
		case client.Send <- message:
		default:
			slog.Warn("client send buffer full, dropping message", "client", clientID)
		}
	}
}

func (h *Hub) Unregister(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client, ok := h.clients[clientID]; ok {
		if client.NoteTitle != "" {
			if subs, ok := h.noteSubs[client.NoteTitle]; ok {
				delete(subs, clientID)
				if len(subs) == 0 {
					delete(h.noteSubs, client.NoteTitle)
				}
			}
		}
		_ = client.Conn.Close()
		close(client.Send)
		delete(h.clients, clientID)
	}
}

func (h *Hub) Subscribe(clientID string, noteTitle string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return
	}

	client, ok := h.clients[clientID]
	if !ok {
		return
	}

	if client.NoteTitle != "" {
		if subs, ok := h.noteSubs[client.NoteTitle]; ok {
			delete(subs, clientID)
		}
	}

	client.NoteTitle = noteTitle
	if h.noteSubs[noteTitle] == nil {
		h.noteSubs[noteTitle] = make(map[string]*Client)
	}
	h.noteSubs[noteTitle][clientID] = client
}

func (h *Hub) BroadcastToNote(noteTitle string, senderID string, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return
	}

	subs, ok := h.noteSubs[noteTitle]
	if !ok {
		return
	}

	for id, client := range subs {
		if id != senderID {
			select {
			case client.Send <- message:
			default:
				slog.Warn("client send buffer full, dropping message", "client", id)
			}
		}
	}
}

func (h *Hub) GetNoteSubscribers(noteTitle string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return 0
	}
	return len(h.noteSubs[noteTitle])
}

func UpgradeHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}
}
