package scheduler

import (
	"context"
	"errors"
	"marvo/internal/control"
	"marvo/internal/scheduling"
	"marvo/internal/store"
	"marvo/internal/userspace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

const schedulerTestSecret = "scheduler-test-secret-that-is-long-enough"

type testExecutor struct {
	started chan store.ClaimedScheduleRun
	block   bool
	fail    bool
	calls   atomic.Int32
}

func (e *testExecutor) Execute(
	ctx context.Context,
	_ string,
	claim store.ClaimedScheduleRun,
	_ Recorder,
) ExecutionResult {
	e.calls.Add(1)
	select {
	case e.started <- claim:
	default:
	}
	if e.block {
		<-ctx.Done()
		return ExecutionResult{Error: context.Cause(ctx)}
	}
	if e.fail {
		return ExecutionResult{Error: errors.New("temporary failure"), Retryable: true}
	}
	return ExecutionResult{}
}

func TestManagerExecutesOneShotAndCompletesIt(t *testing.T) {
	controlDB, layout, userID, workspace := schedulerTestSpace(t)
	executor := &testExecutor{started: make(chan store.ClaimedScheduleRun, 1)}
	created := createDueOneShot(t, workspace, 80*time.Millisecond)

	manager := NewManager(controlDB, layout, executor)
	manager.Start(t.Context())
	defer manager.Close()

	select {
	case claim := <-executor.started:
		if claim.Schedule.ID != created.ID || claim.Run.Trigger != store.ScheduleTriggerScheduled {
			t.Fatalf("claim = %#v", claim)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("automatic task was not executed")
	}

	waitForSchedule(t, workspace, created.ID, func(task store.Schedule, runs []store.ScheduleRun) bool {
		return task.Status == store.ScheduleStatusCompleted && len(runs) == 1 && runs[0].Status == store.ScheduleRunSucceeded
	})
	if executor.calls.Load() != 1 {
		t.Fatalf("execution calls = %d", executor.calls.Load())
	}
	_ = userID
}

func TestManagerStopsActiveRunWithoutRetrying(t *testing.T) {
	controlDB, layout, userID, workspace := schedulerTestSpace(t)
	executor := &testExecutor{started: make(chan store.ClaimedScheduleRun, 1), block: true}
	created := createDueOneShot(t, workspace, 80*time.Millisecond)

	manager := NewManager(controlDB, layout, executor)
	manager.Start(t.Context())
	defer manager.Close()

	var runID string
	select {
	case claim := <-executor.started:
		runID = claim.Run.ID
	case <-time.After(3 * time.Second):
		t.Fatal("automatic task was not started")
	}
	if manager.Stop(userID, created.ID, "11111111111111111111111111111111") {
		t.Fatal("stale run identifier stopped the active task")
	}
	if !manager.Stop(userID, created.ID, runID) {
		t.Fatal("active automatic task was not stopped")
	}
	waitForSchedule(t, workspace, created.ID, func(task store.Schedule, runs []store.ScheduleRun) bool {
		return task.Status == store.ScheduleStatusCompleted && len(runs) == 1 && runs[0].Status == store.ScheduleRunCancelled
	})
	if executor.calls.Load() != 1 {
		t.Fatalf("execution calls = %d", executor.calls.Load())
	}
}

func TestManagerRunsDifferentTasksForOneUserConcurrently(t *testing.T) {
	controlDB, layout, userID, workspace := schedulerTestSpace(t)
	executor := &testExecutor{started: make(chan store.ClaimedScheduleRun, 2), block: true}
	first := createDueOneShot(t, workspace, 100*time.Millisecond)
	second := createDueOneShot(t, workspace, 100*time.Millisecond)

	manager := NewManager(controlDB, layout, executor)
	manager.Start(t.Context())
	defer manager.Close()

	started := make(map[string]string)
	deadline := time.After(3 * time.Second)
	for len(started) < 2 {
		select {
		case claim := <-executor.started:
			started[claim.Schedule.ID] = claim.Run.ID
		case <-deadline:
			t.Fatalf("concurrent tasks started = %#v", started)
		}
	}
	if started[first.ID] == "" || started[second.ID] == "" {
		t.Fatalf("started tasks = %#v", started)
	}
	if !manager.Stop(userID, first.ID, started[first.ID]) || !manager.Stop(userID, second.ID, started[second.ID]) {
		t.Fatal("could not stop both active tasks")
	}
	for _, scheduleID := range []string{first.ID, second.ID} {
		waitForSchedule(t, workspace, scheduleID, func(task store.Schedule, runs []store.ScheduleRun) bool {
			return task.Status == store.ScheduleStatusCompleted && len(runs) == 1 && runs[0].Status == store.ScheduleRunCancelled
		})
	}
}

func TestManagerRetriesThenPausesRepeatedFailure(t *testing.T) {
	controlDB, layout, _, workspace := schedulerTestSpace(t)
	executor := &testExecutor{started: make(chan store.ClaimedScheduleRun, 16), fail: true}
	created := createDueOneShot(t, workspace, 80*time.Millisecond)
	originalDelays := retryDelays
	for index := range retryDelays {
		retryDelays[index] = 10 * time.Millisecond
	}
	t.Cleanup(func() { retryDelays = originalDelays })

	manager := NewManager(controlDB, layout, executor)
	manager.Start(t.Context())
	defer manager.Close()

	waitForSchedule(t, workspace, created.ID, func(task store.Schedule, runs []store.ScheduleRun) bool {
		return task.Status == store.ScheduleStatusPaused && task.PausedReason == "failure" &&
			len(runs) == 1 && runs[0].Status == store.ScheduleRunFailed
	})
	if executor.calls.Load() != int32(len(retryDelays)+1) {
		t.Fatalf("execution calls = %d", executor.calls.Load())
	}
}

func TestFallbackActivityContentStaysValidAndWithinActivityLimit(t *testing.T) {
	input := strings.Repeat("测试", store.MaxActivityContentBytes)
	content := fallbackActivityContent(input)
	if !utf8.ValidString(content) || len(content) > store.MaxActivityContentBytes {
		t.Fatalf("fallback content bytes = %d, valid = %t", len(content), utf8.ValidString(content))
	}
	if !strings.Contains(content, "打开对应的智能体对话") {
		t.Fatalf("fallback content has no continuation hint: %q", content[len(content)-100:])
	}
}

func TestFallbackActivitySkipsTurnThatAlreadyPublishedActivity(t *testing.T) {
	state, err := store.OpenStateDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	manager := &Manager{}
	err = manager.publishFallbackActivity("11111111111111111111", state, store.Schedule{Name: "持续检查"}, ExecutionResult{
		SessionID: "ses_test", MessageID: "msg_answer", FinalText: "本轮已发布活动", ActivityPublished: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	activities, err := store.NewActivityStore(state)
	if err != nil {
		t.Fatal(err)
	}
	page, err := activities.List(30, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Activities) != 0 {
		t.Fatalf("fallback activities = %#v", page.Activities)
	}
}

func schedulerTestSpace(t *testing.T) (*control.DB, *userspace.Layout, string, string) {
	t.Helper()
	layout, err := userspace.OpenLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	controlDB, err := control.Open(layout.ControlDatabase(), schedulerTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controlDB.Close() })
	created, err := controlDB.CreateUser(t.Context(), "Scheduler", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout.EnsureUser(created.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	return controlDB, layout, created.User.ID, paths.Workspace
}

func createDueOneShot(t *testing.T, workspace string, delay time.Duration) store.Schedule {
	t.Helper()
	state, err := store.OpenStateDB(workspace)
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := store.NewScheduleStore(state)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(delay)
	created, err := schedules.Create(t.Context(), store.ScheduleInput{
		Name: "一次检查", Instruction: "检查一次。",
		Definition: scheduling.Definition{Kind: scheduling.KindAt, Spec: scheduling.Spec{At: &at}},
	}, time.Now())
	if closeErr := state.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func waitForSchedule(
	t *testing.T,
	workspace string,
	scheduleID string,
	done func(store.Schedule, []store.ScheduleRun) bool,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := store.OpenStateDB(workspace)
		if err != nil {
			t.Fatal(err)
		}
		schedules, _ := store.NewScheduleStore(state)
		task, taskErr := schedules.Get(t.Context(), scheduleID)
		runs, runsErr := schedules.ListRuns(t.Context(), scheduleID, 30)
		_ = state.Close()
		if taskErr == nil && runsErr == nil && done(task, runs) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("schedule did not reach expected state: task=%#v runs=%#v errors=%v/%v", task, runs, taskErr, runsErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
