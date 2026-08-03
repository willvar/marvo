package collab

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

const replayBufSize = 500

type ReplayMsg struct {
	Seq     int64
	Payload []byte
}

type PollClient struct {
	ID        string
	NoteTitle string
	mu        sync.Mutex
	ringBuf   []ReplayMsg
	writeIdx  int
	nextSeq   int64
	notify    chan struct{}
}

func (pc *PollClient) push(msg []byte) {
	pc.mu.Lock()
	if pc.ringBuf == nil {
		pc.ringBuf = make([]ReplayMsg, replayBufSize)
	}
	idx := pc.writeIdx % replayBufSize
	pc.ringBuf[idx] = ReplayMsg{Seq: pc.nextSeq, Payload: msg}
	pc.writeIdx++
	pc.nextSeq++
	pc.mu.Unlock()
	select {
	case pc.notify <- struct{}{}:
	default:
	}
}

func (pc *PollClient) replaySince(fromSeq int64) []ReplayMsg {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.ringBuf == nil {
		return nil
	}

	start := 0
	count := pc.writeIdx
	if count > replayBufSize {
		start = count - replayBufSize
	}

	var result []ReplayMsg
	for i := start; i < count; i++ {
		msg := pc.ringBuf[i%replayBufSize]
		if msg.Seq > fromSeq {
			result = append(result, msg)
		}
	}
	return result
}

type Hub struct {
	mu           sync.RWMutex
	pollClients  map[string]*PollClient
	notePollSubs map[string]map[string]*PollClient
	done         chan struct{}
	closing      bool
	closeOnce    sync.Once
}

func NewHub() *Hub {
	return &Hub{
		pollClients:  make(map[string]*PollClient),
		notePollSubs: make(map[string]map[string]*PollClient),
		done:         make(chan struct{}),
	}
}

func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closing = true
		h.mu.Unlock()
		close(h.done)
	})
}

func (h *Hub) Done() <-chan struct{} {
	return h.done
}

func (h *Hub) RegisterPoll() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return ""
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	id := hex.EncodeToString(b)
	h.registerPollLocked(id)
	return id
}

func (h *Hub) RegisterPollWithID(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	if _, ok := h.pollClients[id]; ok {
		return true
	}
	h.registerPollLocked(id)
	return true
}

func (h *Hub) registerPollLocked(id string) {
	pc := &PollClient{
		ID:      id,
		nextSeq: 1,
		notify:  make(chan struct{}, 1),
	}
	h.pollClients[id] = pc
}

func (h *Hub) UnregisterPoll(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if pc, ok := h.pollClients[clientID]; ok {
		if pc.NoteTitle != "" {
			if subs, ok := h.notePollSubs[pc.NoteTitle]; ok {
				delete(subs, clientID)
				if len(subs) == 0 {
					delete(h.notePollSubs, pc.NoteTitle)
				}
			}
		}
		delete(h.pollClients, clientID)
	}
}

func (h *Hub) PollSubscribe(clientID string, noteTitle string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return
	}

	pc, ok := h.pollClients[clientID]
	if !ok {
		return
	}

	if pc.NoteTitle != "" {
		if subs, ok := h.notePollSubs[pc.NoteTitle]; ok {
			delete(subs, clientID)
			if len(subs) == 0 {
				delete(h.notePollSubs, pc.NoteTitle)
			}
		}
	}

	pc.NoteTitle = noteTitle
	if noteTitle == "" {
		return
	}
	if h.notePollSubs[noteTitle] == nil {
		h.notePollSubs[noteTitle] = make(map[string]*PollClient)
	}
	h.notePollSubs[noteTitle][clientID] = pc
}

func (h *Hub) PollUnsubscribe(clientID, noteTitle string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pc, ok := h.pollClients[clientID]
	if !ok || (noteTitle != "" && pc.NoteTitle != noteTitle) {
		return
	}
	if subs := h.notePollSubs[pc.NoteTitle]; subs != nil {
		delete(subs, clientID)
		if len(subs) == 0 {
			delete(h.notePollSubs, pc.NoteTitle)
		}
	}
	pc.NoteTitle = ""
}

// MoveNote keeps live subscriptions attached when a rename is performed by
// Marvo. Out-of-band filesystem renames are intentionally treated as a fresh
// instance unless the watcher can prove otherwise.
func (h *Hub) MoveNote(oldTitle, newTitle string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.notePollSubs[oldTitle]
	if len(subs) == 0 {
		return
	}
	if h.notePollSubs[newTitle] == nil {
		h.notePollSubs[newTitle] = make(map[string]*PollClient)
	}
	for id, pc := range subs {
		pc.NoteTitle = newTitle
		h.notePollSubs[newTitle][id] = pc
	}
	delete(h.notePollSubs, oldTitle)
}

func (h *Hub) PollClientExists(clientID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.pollClients[clientID]
	return ok
}

func (h *Hub) SendToPollClient(clientID string, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return
	}
	if pc, ok := h.pollClients[clientID]; ok {
		pc.push(message)
	}
}

func (h *Hub) PollNotifyChan(clientID string) <-chan struct{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if pc, ok := h.pollClients[clientID]; ok {
		return pc.notify
	}
	return nil
}

func (h *Hub) PollReplaySince(clientID string, fromSeq int64) []ReplayMsg {
	h.mu.RLock()
	pc, ok := h.pollClients[clientID]
	h.mu.RUnlock()
	if !ok {
		return nil
	}
	return pc.replaySince(fromSeq)
}

func (h *Hub) BroadcastAll(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return
	}
	for _, pc := range h.pollClients {
		pc.push(message)
	}
}

func (h *Hub) BroadcastToNote(noteTitle string, senderID string, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closing {
		return
	}

	pollSubs, ok := h.notePollSubs[noteTitle]
	if ok {
		for id, pc := range pollSubs {
			if id != senderID {
				pc.push(message)
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
	return len(h.notePollSubs[noteTitle])
}
