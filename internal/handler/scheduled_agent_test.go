package handler

import (
	"context"
	"encoding/json"
	"errors"
	"marvo/internal/scheduling"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type noOpScheduleRecorder struct{}

func (noOpScheduleRecorder) SetSession(context.Context, string) error        { return nil }
func (noOpScheduleRecorder) SetRequestMessage(context.Context, string) error { return nil }
func (noOpScheduleRecorder) SetResponseMessage(context.Context, string) error {
	return nil
}

type recordingScheduleRecorder struct {
	noOpScheduleRecorder
	requestMessageID string
}

func (r *recordingScheduleRecorder) SetRequestMessage(_ context.Context, messageID string) error {
	r.requestMessageID = messageID
	return nil
}

func TestScheduledCompletionWaitsForIdleBeforeAcceptingAssistantText(t *testing.T) {
	var busy atomic.Bool
	busy.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/ses_test/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{"info": map[string]any{"id": "msg_request", "sessionID": "ses_test", "role": "user"}, "parts": []any{}},
				map[string]any{
					"info":  map[string]any{"id": "msg_answer", "sessionID": "ses_test", "role": "assistant", "parentID": "msg_request"},
					"parts": []any{map[string]any{"type": "text", "text": "仍在生成的内容"}},
				},
			})
		case "/session/status":
			status := "idle"
			if busy.Load() {
				status = "busy"
			}
			writeJSON(w, http.StatusOK, map[string]any{"ses_test": map[string]any{"type": status}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	completion, err := deps.scheduledCompletion(context.Background(), "ses_test", "msg_request")
	if err != nil {
		t.Fatal(err)
	}
	if completion.Found || !completion.RequestFound {
		t.Fatalf("busy completion was accepted: %#v", completion)
	}

	busy.Store(false)
	completion, err = deps.scheduledCompletion(context.Background(), "ses_test", "msg_request")
	if err != nil {
		t.Fatal(err)
	}
	if !completion.RequestFound || !completion.Found || completion.MessageID != "msg_answer" || completion.Text != "仍在生成的内容" {
		t.Fatalf("idle completion = %#v", completion)
	}
}

func TestScheduledCompletionDetectsActivityPublishedEarlierInTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/ses_test/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{"info": map[string]any{"id": "msg_request", "sessionID": "ses_test", "role": "user"}, "parts": []any{}},
				map[string]any{
					"info": map[string]any{"id": "msg_tool", "sessionID": "ses_test", "role": "assistant", "parentID": "msg_request"},
					"parts": []any{map[string]any{
						"type": "tool", "tool": "marvo_activity",
						"state": map[string]any{"status": "completed"},
					}},
				},
				map[string]any{
					"info":  map[string]any{"id": "msg_answer", "sessionID": "ses_test", "role": "assistant", "parentID": "msg_request"},
					"parts": []any{map[string]any{"type": "text", "text": "本轮已发布活动"}},
				},
			})
		case "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{"ses_test": map[string]any{"type": "idle"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	completion, err := deps.scheduledCompletion(context.Background(), "ses_test", "msg_request")
	if err != nil {
		t.Fatal(err)
	}
	if !completion.Found || completion.MessageID != "msg_answer" || !completion.ActivityPublished {
		t.Fatalf("completion = %#v", completion)
	}
}

func TestScheduledCompletionRejectsPersistentlyOrphanedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/ses_test/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{"info": map[string]any{"id": "msg_request", "sessionID": "ses_test", "role": "user"}, "parts": []any{}},
			})
		case "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{"ses_test": map[string]any{"type": "idle"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	started := time.Now()
	_, err := deps.waitScheduledCompletion(t.Context(), "ses_test", "msg_request")
	if !errors.Is(err, errScheduledResponseMissing) {
		t.Fatalf("waitScheduledCompletion() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("orphan detection took %v", elapsed)
	}
}

func TestScheduledSystemPromptCarriesTrustedRunContext(t *testing.T) {
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	claim := store.ClaimedScheduleRun{
		Schedule: store.Schedule{
			ID: "11111111111111111111111111111111", Name: "持续研究", Revision: 7,
			Definition: scheduling.Definition{
				Kind: scheduling.KindAdaptive,
				Spec: scheduling.Spec{MinimumSeconds: 60, DefaultSeconds: 300, MaximumSeconds: 3600},
			},
		},
		Run: store.ScheduleRun{ID: "22222222222222222222222222222222", ScheduledFor: now},
	}
	prompt := scheduledSystemPrompt(claim)
	for _, expected := range []string{
		"marvo_activity", "marvo_schedules", "schedule_id 和 revision", "next_check", "委派子任务", scheduledNoActivityMarker,
		`"schedule_id":"11111111111111111111111111111111"`, `"run_id":"22222222222222222222222222222222"`, `"revision":7`,
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("scheduled prompt missing %q: %s", expected, prompt)
		}
	}
	var contextPayload map[string]any
	parts := strings.SplitN(prompt, "\n", 2)
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &contextPayload) != nil {
		t.Fatalf("scheduled prompt context is not valid JSON: %q", prompt)
	}
}

func TestScheduledExecutionResumesExistingBusyRequestWithoutSendingAgain(t *testing.T) {
	var busy atomic.Bool
	busy.Store(true)
	var promptCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_existing":
			writeJSON(w, http.StatusOK, map[string]any{"id": "ses_existing", "title": "持续检查"})
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_existing/message":
			writeJSON(w, http.StatusOK, []any{
				map[string]any{"info": map[string]any{"id": "msg_existing", "sessionID": "ses_existing", "role": "user"}, "parts": []any{}},
				map[string]any{
					"info": map[string]any{"id": "msg_activity", "sessionID": "ses_existing", "role": "assistant", "parentID": "msg_existing"},
					"parts": []any{map[string]any{
						"type": "tool", "tool": "marvo_activity",
						"state": map[string]any{"status": "completed"},
					}},
				},
				map[string]any{
					"info":  map[string]any{"id": "msg_answer", "sessionID": "ses_existing", "role": "assistant", "parentID": "msg_existing"},
					"parts": []any{map[string]any{"type": "text", "text": "恢复后的完整结果"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/session/status":
			status := "idle"
			if busy.Load() {
				status = "busy"
			}
			writeJSON(w, http.StatusOK, map[string]any{"ses_existing": map[string]any{"type": status}})
		case r.Method == http.MethodGet && r.URL.Path == "/global/event":
			busy.Store(false)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"payload\":{\"type\":\"session.idle\",\"properties\":{\"sessionID\":\"ses_existing\"}}}\n\n"))
		case r.Method == http.MethodPost && r.URL.Path == "/session/ses_existing/prompt_async":
			promptCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	result := deps.ExecuteSchedule(context.Background(), store.ClaimedScheduleRun{
		Schedule: store.Schedule{ID: "11111111111111111111111111111111", Name: "持续检查", SessionID: "ses_existing"},
		Run: store.ScheduleRun{
			ID: "22222222222222222222222222222222", RequestMessageID: "msg_existing", ScheduledFor: time.Now().UTC(),
		},
	}, noOpScheduleRecorder{})
	if result.Error != nil || result.MessageID != "msg_answer" || result.FinalText != "恢复后的完整结果" || !result.ActivityPublished {
		t.Fatalf("resumed execution = %#v", result)
	}
	if promptCalls.Load() != 0 {
		t.Fatalf("prompt calls = %d", promptCalls.Load())
	}
}

func TestScheduledExecutionReplacesOrphanedRequestMessage(t *testing.T) {
	const runID = "22222222222222222222222222222222"
	var requestID atomic.Value
	requestID.Store("msg_old")
	var answerReady atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_existing":
			writeJSON(w, http.StatusOK, map[string]any{"id": "ses_existing", "title": "持续检查"})
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_existing/message":
			messages := []any{
				map[string]any{"info": map[string]any{"id": requestID.Load().(string), "sessionID": "ses_existing", "role": "user"}, "parts": []any{}},
			}
			if answerReady.Load() {
				messages = append(messages, map[string]any{
					"info":  map[string]any{"id": "msg_recovered_answer", "sessionID": "ses_existing", "role": "assistant", "parentID": requestID.Load().(string)},
					"parts": []any{map[string]any{"type": "text", "text": "重新执行后的结果"}},
				})
			}
			writeJSON(w, http.StatusOK, messages)
		case r.Method == http.MethodGet && r.URL.Path == "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{"ses_existing": map[string]any{"type": "idle"}})
		case r.Method == http.MethodPost && r.URL.Path == "/session/ses_existing/prompt_async":
			var body struct {
				MessageID string `json:"messageID"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			requestID.Store(body.MessageID)
			answerReady.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	recorder := &recordingScheduleRecorder{}
	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	result := deps.ExecuteSchedule(context.Background(), store.ClaimedScheduleRun{
		Schedule: store.Schedule{ID: "11111111111111111111111111111111", Name: "持续检查", SessionID: "ses_existing"},
		Run: store.ScheduleRun{
			ID: runID, Attempt: 2, RequestMessageID: "msg_old", ScheduledFor: time.Now().UTC(),
		},
	}, recorder)
	wantRequestID := "msg_" + runID + "_2"
	if result.Error != nil || result.MessageID != "msg_recovered_answer" || result.FinalText != "重新执行后的结果" {
		t.Fatalf("recovered execution = %#v", result)
	}
	if recorder.requestMessageID != wantRequestID || requestID.Load().(string) != wantRequestID {
		t.Fatalf("replacement request = %q / %q, want %q", recorder.requestMessageID, requestID.Load(), wantRequestID)
	}
}

func TestScheduledExecutionRetriesRetryableAssistantFailureWithNewMessage(t *testing.T) {
	const runID = "55555555555555555555555555555555"
	var requestID atomic.Value
	requestID.Store("msg_failed")
	var answerReady atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_retry":
			writeJSON(w, http.StatusOK, map[string]any{"id": "ses_retry", "title": "重试检查"})
		case r.Method == http.MethodGet && r.URL.Path == "/session/ses_retry/message":
			messages := []any{
				map[string]any{"info": map[string]any{"id": requestID.Load().(string), "sessionID": "ses_retry", "role": "user"}, "parts": []any{}},
			}
			if answerReady.Load() {
				messages = append(messages, map[string]any{
					"info":  map[string]any{"id": "msg_retry_answer", "sessionID": "ses_retry", "role": "assistant", "parentID": requestID.Load().(string)},
					"parts": []any{map[string]any{"type": "text", "text": "重试成功"}},
				})
			} else {
				messages = append(messages, map[string]any{
					"info": map[string]any{
						"id": "msg_failed_answer", "sessionID": "ses_retry", "role": "assistant", "parentID": "msg_failed",
						"error": map[string]any{"message": "请求过多", "isRetryable": true},
					},
					"parts": []any{},
				})
			}
			writeJSON(w, http.StatusOK, messages)
		case r.Method == http.MethodGet && r.URL.Path == "/session/status":
			writeJSON(w, http.StatusOK, map[string]any{"ses_retry": map[string]any{"type": "idle"}})
		case r.Method == http.MethodPost && r.URL.Path == "/session/ses_retry/prompt_async":
			var body struct {
				MessageID string `json:"messageID"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			requestID.Store(body.MessageID)
			answerReady.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	recorder := &recordingScheduleRecorder{}
	deps := NewAgentDeps(server.URL, make(chan struct{}), nil, nil, nil, nil)
	result := deps.ExecuteSchedule(context.Background(), store.ClaimedScheduleRun{
		Schedule: store.Schedule{ID: "66666666666666666666666666666666", Name: "重试检查", SessionID: "ses_retry"},
		Run: store.ScheduleRun{
			ID: runID, Attempt: 2, RequestMessageID: "msg_failed", ScheduledFor: time.Now().UTC(),
		},
	}, recorder)
	wantRequestID := "msg_" + runID + "_2"
	if result.Error != nil || result.MessageID != "msg_retry_answer" || result.FinalText != "重试成功" {
		t.Fatalf("retried execution = %#v", result)
	}
	if recorder.requestMessageID != wantRequestID || requestID.Load().(string) != wantRequestID {
		t.Fatalf("retry request = %q / %q, want %q", recorder.requestMessageID, requestID.Load(), wantRequestID)
	}
}
