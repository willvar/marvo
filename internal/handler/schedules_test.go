package handler

import (
	"bytes"
	"encoding/json"
	"marvo/internal/scheduling"
	"marvo/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScheduleHandlersLifecycleAndConflict(t *testing.T) {
	schedules, err := store.NewScheduleStore(newHandlerStateDB(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{Schedules: schedules}

	create := httptest.NewRecorder()
	deps.CreateSchedule(create, httptest.NewRequest(http.MethodPost, "/api/schedules", strings.NewReader(`{
		"name":"跟进研究进展",
		"instruction":"检查最新进展，有值得关注的变化时发布活动。",
		"schedule":{"kind":"every","spec":{"every_seconds":3600}}
	}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created scheduleResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Revision != 1 || created.NextRunAt == nil {
		t.Fatalf("created schedule = %#v", created.Schedule)
	}

	list := httptest.NewRecorder()
	deps.ListSchedules(list, httptest.NewRequest(http.MethodGet, "/api/schedules", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.ID) {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}

	conflict := httptest.NewRecorder()
	conflictRequest := httptest.NewRequest(http.MethodPut, "/api/schedules/"+created.ID, strings.NewReader(`{
		"revision":99,
		"name":"冲突更新",
		"instruction":"不应被保存",
		"schedule":{"kind":"every","spec":{"every_seconds":7200}}
	}`))
	conflictRequest.SetPathValue("id", created.ID)
	deps.UpdateSchedule(conflict, conflictRequest)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}

	pause := httptest.NewRecorder()
	pauseRequest := httptest.NewRequest(http.MethodPost, "/api/schedules/"+created.ID+"/pause", strings.NewReader(`{"revision":1}`))
	pauseRequest.SetPathValue("id", created.ID)
	deps.PauseSchedule(pause, pauseRequest)
	if pause.Code != http.StatusOK {
		t.Fatalf("pause status = %d, body = %s", pause.Code, pause.Body.String())
	}
	var paused scheduleResponse
	if err := json.NewDecoder(pause.Body).Decode(&paused); err != nil {
		t.Fatal(err)
	}
	if paused.Status != store.ScheduleStatusPaused || paused.NextRunAt != nil {
		t.Fatalf("paused schedule = %#v", paused.Schedule)
	}

	resume := httptest.NewRecorder()
	resumeRequest := httptest.NewRequest(http.MethodPost, "/api/schedules/"+created.ID+"/resume", strings.NewReader(`{"revision":2}`))
	resumeRequest.SetPathValue("id", created.ID)
	deps.ResumeSchedule(resume, resumeRequest)
	if resume.Code != http.StatusOK {
		t.Fatalf("resume status = %d, body = %s", resume.Code, resume.Body.String())
	}
	var resumed scheduleResponse
	if err := json.NewDecoder(resume.Body).Decode(&resumed); err != nil {
		t.Fatal(err)
	}

	staleDelete := httptest.NewRecorder()
	staleDeleteRequest := httptest.NewRequest(http.MethodDelete, "/api/schedules/"+created.ID, strings.NewReader(`{"revision":2}`))
	staleDeleteRequest.SetPathValue("id", created.ID)
	deps.DeleteSchedule(staleDelete, staleDeleteRequest)
	if staleDelete.Code != http.StatusConflict {
		t.Fatalf("stale delete status = %d, body = %s", staleDelete.Code, staleDelete.Body.String())
	}

	remove := httptest.NewRecorder()
	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/schedules/"+created.ID, strings.NewReader(`{"revision":3}`))
	removeRequest.SetPathValue("id", created.ID)
	deps.DeleteSchedule(remove, removeRequest)
	if remove.Code != http.StatusNoContent || resumed.Revision != 3 {
		t.Fatalf("delete status = %d, resumed = %#v, body = %s", remove.Code, resumed, remove.Body.String())
	}
}

func TestScheduleHandlersExposeAndProtectActiveRun(t *testing.T) {
	schedules, err := store.NewScheduleStore(newHandlerStateDB(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{Schedules: schedules}
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	created, err := schedules.Create(t.Context(), store.ScheduleInput{
		Name: "每日摘要", Instruction: "整理每日摘要。",
		Definition: scheduling.Definition{
			Kind: scheduling.KindEvery,
			Spec: scheduling.Spec{EverySeconds: 3600},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	run := httptest.NewRecorder()
	runRequest := httptest.NewRequest(http.MethodPost, "/api/schedules/"+created.ID+"/run", nil)
	runRequest.SetPathValue("id", created.ID)
	deps.RunScheduleNow(run, runRequest)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %s", run.Code, run.Body.String())
	}

	get := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/schedules/"+created.ID, nil)
	getRequest.SetPathValue("id", created.ID)
	deps.GetSchedule(get, getRequest)
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
	}
	var response scheduleResponse
	if err := json.NewDecoder(get.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ActiveRun == nil || response.ActiveRun.Status != store.ScheduleRunQueued {
		t.Fatalf("active run = %#v", response.ActiveRun)
	}

	remove := httptest.NewRecorder()
	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/schedules/"+created.ID, strings.NewReader(`{"revision":1}`))
	removeRequest.SetPathValue("id", created.ID)
	deps.DeleteSchedule(remove, removeRequest)
	if remove.Code != http.StatusConflict {
		t.Fatalf("delete busy status = %d, body = %s", remove.Code, remove.Body.String())
	}

	stop := httptest.NewRecorder()
	stopRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/schedules/"+created.ID+"/stop",
		strings.NewReader(`{"run_id":"`+response.ActiveRun.ID+`"}`),
	)
	stopRequest.SetPathValue("id", created.ID)
	deps.StopScheduleRun(stop, stopRequest)
	if stop.Code != http.StatusAccepted {
		t.Fatalf("stop queued status = %d, body = %s", stop.Code, stop.Body.String())
	}
}

func TestScheduleHandlerAcceptsEscapedInstructionWithinDecodedLimit(t *testing.T) {
	schedules, err := store.NewScheduleStore(newHandlerStateDB(t))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"name":        "长任务内容",
		"instruction": strings.Repeat("a\n", 30_000),
		"schedule": map[string]any{
			"kind": "every",
			"spec": map[string]any{"every_seconds": 3600},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= store.MaxScheduleInstructionBytes+16<<10 {
		t.Fatalf("test request did not exceed the former body limit: %d bytes", len(payload))
	}
	response := httptest.NewRecorder()
	(&Dependencies{Schedules: schedules}).CreateSchedule(
		response,
		httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewReader(payload)),
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
}
