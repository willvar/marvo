package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fakeSession struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	ParentID string           `json:"parentID,omitempty"`
	Time     map[string]int64 `json:"time"`
}

type fakeMessage struct {
	Info  map[string]any   `json:"info"`
	Parts []map[string]any `json:"parts"`
}

type fakeState struct {
	mu       sync.Mutex
	nextID   int
	sessions map[string]fakeSession
	messages map[string][]fakeMessage
	statuses map[string]map[string]string
}

func newFakeState() *fakeState {
	return &fakeState{
		sessions: make(map[string]fakeSession),
		messages: make(map[string][]fakeMessage),
		statuses: make(map[string]map[string]string),
	}
}

func (s *fakeState) listSessions() []fakeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]fakeSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result
}

func (s *fakeState) createSession() fakeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	now := time.Now().UnixMilli()
	id := "ses_e2e_" + strconv.Itoa(s.nextID)
	session := fakeSession{ID: id, Title: "New session - " + time.Now().UTC().Format(time.RFC3339Nano), Time: map[string]int64{"created": now, "updated": now}}
	s.sessions[id] = session
	s.statuses[id] = map[string]string{"type": "idle"}
	return session
}

func (s *fakeState) getSession(id string) (fakeSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *fakeState) updateSessionTitle(id, title string) (fakeSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return fakeSession{}, false
	}
	session.Title = title
	session.Time["updated"] = time.Now().UnixMilli()
	s.sessions[id] = session
	return session, true
}

func (s *fakeState) addPrompt(id string, body map[string]any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	rawParts, ok := body["parts"].([]any)
	if !ok || len(rawParts) == 0 {
		return false
	}
	messageID := fmt.Sprintf("msg_%d", len(s.messages[id])+1)
	parts := make([]map[string]any, 0, len(rawParts))
	for index, raw := range rawParts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		copyPart := make(map[string]any, len(part)+3)
		for key, value := range part {
			copyPart[key] = value
		}
		copyPart["id"] = fmt.Sprintf("part_%s_%d", messageID, index+1)
		copyPart["messageID"] = messageID
		copyPart["sessionID"] = id
		parts = append(parts, copyPart)
	}
	if len(parts) == 0 {
		return false
	}
	messageInfo := map[string]any{
		"id": messageID, "sessionID": id, "role": "user",
		"time": map[string]int64{"created": time.Now().UnixMilli()},
	}
	if model, ok := body["model"].(map[string]any); ok {
		messageInfo["model"] = model
	}
	if system, ok := body["system"].(string); ok {
		messageInfo["system"] = system
	}
	s.messages[id] = append(s.messages[id], fakeMessage{
		Info:  messageInfo,
		Parts: parts,
	})
	s.statuses[id] = map[string]string{"type": "busy"}
	return true
}

func (s *fakeState) sessionMessages(id string) []fakeMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeMessage(nil), s.messages[id]...)
}

func (s *fakeState) allStatuses() map[string]map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]map[string]string, len(s.statuses))
	for id, status := range s.statuses {
		result[id] = map[string]string{"type": status["type"]}
	}
	return result
}

func (s *fakeState) abort(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	s.statuses[id] = map[string]string{"type": "idle"}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	addr := os.Getenv("MARVO_FAKE_OPENCODE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:15096"
	}
	state := newFakeState()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /provider", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected": []string{"fake"},
			"default":   map[string]string{"fake": "vision"},
			"all": []any{
				map[string]any{
					"id": "fake", "name": "E2E Provider",
					"models": map[string]any{
						"vision": map[string]any{
							"id": "vision", "providerID": "fake", "name": "E2E Vision", "family": "vision", "status": "active",
							"variants": map[string]any{
								"low":  map[string]string{"reasoningEffort": "low"},
								"high": map[string]string{"reasoningEffort": "high"},
							},
							"capabilities": map[string]any{
								"attachment": true, "reasoning": true, "toolcall": true,
								"input":  map[string]bool{"text": true, "audio": false, "image": true, "video": true, "pdf": true},
								"output": map[string]bool{"text": true, "audio": false, "image": false, "video": false, "pdf": false},
							},
							"limit": map[string]int{"context": 128000, "output": 16000},
						},
						"text": map[string]any{
							"id": "text", "providerID": "fake", "name": "E2E Text", "family": "text", "status": "active",
							"capabilities": map[string]any{
								"attachment": false, "reasoning": false, "toolcall": true,
								"input":  map[string]bool{"text": true, "audio": false, "image": false, "video": false, "pdf": false},
								"output": map[string]bool{"text": true, "audio": false, "image": false, "video": false, "pdf": false},
							},
							"limit": map[string]int{"context": 32000, "output": 8000},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"model": "fake/vision"})
	})
	mux.HandleFunc("GET /global/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"payload\":{\"type\":\"server.connected\",\"properties\":{}}}\n\n")
		flusher.Flush()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, state.listSessions())
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, state.createSession())
	})
	mux.HandleFunc("GET /session/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, state.allStatuses())
	})
	mux.HandleFunc("GET /session/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := state.getSession(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("PATCH /session/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title string `json:"title"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Title) == "" {
			http.Error(w, "invalid title", http.StatusBadRequest)
			return
		}
		session, ok := state.updateSessionTitle(r.PathValue("id"), body.Title)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("GET /session/{id}/message", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := state.getSession(r.PathValue("id")); !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, state.sessionMessages(r.PathValue("id")))
	})
	mux.HandleFunc("POST /session/{id}/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || !state.addPrompt(r.PathValue("id"), body) {
			http.Error(w, "invalid prompt", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /session/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		if !state.abort(r.PathValue("id")) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, true)
	})
	for _, path := range []string{"/permission", "/question"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, []any{})
		})
	}
	mux.HandleFunc("POST /permission/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reply string `json:"reply"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || (body.Reply != "once" && body.Reply != "always" && body.Reply != "reject") {
			http.Error(w, "invalid permission reply", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, true)
	})
	mux.HandleFunc("POST /question/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Answers [][]string `json:"answers"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Answers) == 0 {
			http.Error(w, "invalid question reply", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, true)
	})
	mux.HandleFunc("POST /question/{id}/reject", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, true)
	})
	// Keep the fake deliberately strict: unexpected OpenCode calls should fail
	// visibly instead of making browser tests pass against an unrealistic API.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unhandled fake OpenCode route: "+r.Method+" "+strings.TrimSpace(r.URL.Path), http.StatusNotFound)
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
