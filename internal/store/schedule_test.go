package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"marvo/internal/scheduling"
)

func testEverySchedule(name string, every time.Duration) ScheduleInput {
	return ScheduleInput{
		Name: name, Instruction: "检查资料并发布重要进展。",
		Definition: scheduling.Definition{
			Kind: scheduling.KindEvery,
			Spec: scheduling.Spec{EverySeconds: int64(every / time.Second)},
		},
	}
}

func TestScheduleStoreLifecycleAndOptimisticRevision(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

	created, err := store.Create(ctx, testEverySchedule("持续研究", time.Hour), now)
	if err != nil || created.Status != ScheduleStatusActive || created.NextRunAt == nil || !created.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	updated, err := store.Update(ctx, created.ID, created.Revision, testEverySchedule("持续研究新主题", 2*time.Hour), now.Add(time.Minute))
	if err != nil || updated.Revision != created.Revision+1 || updated.Name != "持续研究新主题" {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	if _, err := store.Update(ctx, created.ID, created.Revision, testEverySchedule("冲突", time.Hour), now); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	paused, err := store.Pause(ctx, updated.ID, updated.Revision, "user", now.Add(2*time.Minute))
	if err != nil || paused.Status != ScheduleStatusPaused || paused.NextRunAt != nil {
		t.Fatalf("Pause() = %#v, %v", paused, err)
	}
	resumed, err := store.Resume(ctx, paused.ID, paused.Revision, now.Add(3*time.Minute))
	if err != nil || resumed.Status != ScheduleStatusActive || resumed.NextRunAt == nil {
		t.Fatalf("Resume() = %#v, %v", resumed, err)
	}
	if _, err := store.Delete(ctx, resumed.ID, paused.Revision); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("stale delete error = %v", err)
	}
	if deleted, err := store.Delete(ctx, resumed.ID, resumed.Revision); err != nil || !deleted {
		t.Fatalf("Delete() = %t, %v", deleted, err)
	}
}

func TestScheduleStoreResumesOverdueOneShotImmediately(t *testing.T) {
	state, _ := newTestStateDB(t)
	schedules, _ := NewScheduleStore(state)
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	at := now.Add(time.Minute)
	created, err := schedules.Create(t.Context(), ScheduleInput{
		Name: "单次检查", Instruction: "检查一次。",
		Definition: scheduling.Definition{Kind: scheduling.KindAt, Spec: scheduling.Spec{At: &at}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := schedules.Pause(t.Context(), created.ID, created.Revision, "稍后继续", now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	resumedAt := now.Add(2 * time.Minute)
	resumed, err := schedules.Resume(t.Context(), paused.ID, paused.Revision, resumedAt)
	if err != nil || resumed.NextRunAt == nil || !resumed.NextRunAt.Equal(resumedAt) {
		t.Fatalf("Resume() = %#v, %v", resumed, err)
	}
}

func TestScheduleStoreRefusesDeleteWhileRunIsPending(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := store.Create(ctx, testEverySchedule("正在执行", time.Hour), now)
	if _, err := store.RunNow(ctx, created.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Delete(ctx, created.ID, created.Revision); !errors.Is(err, ErrScheduleBusy) {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestScheduleStoreManualFailureDoesNotPauseNaturalSchedule(t *testing.T) {
	state, _ := newTestStateDB(t)
	schedules, _ := NewScheduleStore(state)
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := schedules.Create(t.Context(), testEverySchedule("手动检查", time.Hour), now)
	_, _ = schedules.RunNow(t.Context(), created.ID, now.Add(time.Minute))
	claim, err := schedules.ClaimDue(t.Context(), now.Add(time.Minute), 2*time.Minute)
	if err != nil || claim == nil {
		t.Fatalf("manual claim = %#v, %v", claim, err)
	}
	updated, run, retrying, err := schedules.FinishFailure(t.Context(), *claim, ScheduleFailure{
		Error: "manual failure", Retryable: false, MaxAttempts: 1,
	}, now.Add(2*time.Minute))
	if err != nil || retrying || run.Status != ScheduleRunFailed {
		t.Fatalf("manual failure = %#v, %t, %v", run, retrying, err)
	}
	if updated.Status != ScheduleStatusActive || updated.NextRunAt == nil || !updated.NextRunAt.Equal(*created.NextRunAt) {
		t.Fatalf("manual failure changed natural schedule: %#v", updated)
	}
}

func TestScheduleStoreOldRunFailureDoesNotPolluteUpdatedTask(t *testing.T) {
	state, _ := newTestStateDB(t)
	schedules, _ := NewScheduleStore(state)
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := schedules.Create(t.Context(), testEverySchedule("原任务", time.Hour), now)
	_, _ = schedules.RunNow(t.Context(), created.ID, now.Add(time.Minute))
	claim, err := schedules.ClaimDue(t.Context(), now.Add(time.Minute), 2*time.Minute)
	if err != nil || claim == nil {
		t.Fatalf("manual claim = %#v, %v", claim, err)
	}
	changed, err := schedules.Update(
		t.Context(), created.ID, created.Revision, testEverySchedule("新任务", 2*time.Hour), now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	updated, _, _, err := schedules.FinishFailure(t.Context(), *claim, ScheduleFailure{
		Error: "failure from old revision", Retryable: false, MaxAttempts: 1,
	}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != changed.Revision || updated.Name != "新任务" || updated.Status != ScheduleStatusActive ||
		updated.LastError != "" || updated.ConsecutiveFailures != 0 || updated.NextRunAt == nil ||
		!updated.NextRunAt.Equal(*changed.NextRunAt) {
		t.Fatalf("old failure polluted updated task: %#v", updated)
	}
}

func TestScheduleStoreCancelsPendingManualRun(t *testing.T) {
	state, _ := newTestStateDB(t)
	schedules, _ := NewScheduleStore(state)
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := schedules.Create(t.Context(), testEverySchedule("等待执行", time.Hour), now)
	run, err := schedules.RunNow(t.Context(), created.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := schedules.CancelPendingRun(t.Context(), created.ID, run.ID, now.Add(2*time.Minute))
	if err != nil || !cancelled {
		t.Fatalf("CancelPendingRun() = %t, %v", cancelled, err)
	}
	updatedRun, err := schedules.GetRun(t.Context(), run.ID)
	if err != nil || updatedRun.Status != ScheduleRunCancelled {
		t.Fatalf("cancelled run = %#v, %v", updatedRun, err)
	}
	updated, _ := schedules.Get(t.Context(), created.ID)
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(*created.NextRunAt) {
		t.Fatalf("manual cancellation moved natural occurrence: %#v", updated)
	}
	newRun, err := schedules.RunNow(t.Context(), created.ID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled, err := schedules.CancelPendingRun(t.Context(), created.ID, run.ID, now.Add(4*time.Minute)); err != nil || cancelled {
		t.Fatalf("stale cancellation = %t, %v", cancelled, err)
	}
	currentRun, err := schedules.GetRun(t.Context(), newRun.ID)
	if err != nil || currentRun.Status != ScheduleRunQueued {
		t.Fatalf("new run was changed by stale cancellation: %#v, %v", currentRun, err)
	}
}

func TestScheduleStoreClaimsOneRunAndAdvancesAfterSuccess(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, err := store.Create(ctx, testEverySchedule("每小时检查", time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimDue(ctx, now.Add(time.Hour), 2*time.Minute)
	if err != nil || claim == nil || claim.Schedule.ID != created.ID || claim.Run.Attempt != 1 {
		t.Fatalf("ClaimDue() = %#v, %v", claim, err)
	}
	if duplicate, err := store.ClaimDue(ctx, now.Add(time.Hour), 2*time.Minute); err != nil || duplicate != nil {
		t.Fatalf("duplicate claim = %#v, %v", duplicate, err)
	}
	finishedAt := now.Add(time.Hour + 10*time.Minute)
	updated, run, err := store.FinishSuccess(ctx, *claim, finishedAt)
	if err != nil || run.Status != ScheduleRunSucceeded || updated.NextRunAt == nil || !updated.NextRunAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("FinishSuccess() = %#v, %#v, %v", updated, run, err)
	}
}

func TestScheduleStoreRetriesSameDurableRunThenPauses(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := store.Create(ctx, testEverySchedule("失败重试", time.Hour), now)
	claim, _ := store.ClaimDue(ctx, now.Add(time.Hour), 2*time.Minute)
	failedAt := now.Add(time.Hour + time.Minute)
	updated, retrying, retry, err := store.FinishFailure(ctx, *claim, ScheduleFailure{
		Error: "temporary", Retryable: true, RetryDelay: 30 * time.Second, MaxAttempts: 2,
	}, failedAt)
	if err != nil || !retry || retrying.Status != ScheduleRunWaitingRetry || updated.Status != ScheduleStatusActive {
		t.Fatalf("first failure = %#v, %#v, %t, %v", updated, retrying, retry, err)
	}
	retryClaim, err := store.ClaimDue(ctx, failedAt.Add(30*time.Second), 2*time.Minute)
	if err != nil || retryClaim == nil || retryClaim.Run.ID != claim.Run.ID || retryClaim.Run.Attempt != 2 {
		t.Fatalf("retry claim = %#v, %v", retryClaim, err)
	}
	updated, terminal, retry, err := store.FinishFailure(ctx, *retryClaim, ScheduleFailure{
		Error: "still failing", Retryable: true, RetryDelay: time.Minute, MaxAttempts: 2,
	}, failedAt.Add(time.Minute))
	if err != nil || retry || terminal.Status != ScheduleRunFailed || updated.Status != ScheduleStatusPaused || updated.ID != created.ID {
		t.Fatalf("terminal failure = %#v, %#v, %t, %v", updated, terminal, retry, err)
	}
}

func TestScheduleStoreManualRunDoesNotMoveNaturalOccurrence(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := store.Create(ctx, testEverySchedule("手动运行", time.Hour), now)
	manual, err := store.RunNow(ctx, created.ID, now.Add(time.Minute))
	if err != nil || manual.Trigger != ScheduleTriggerManual {
		t.Fatalf("RunNow() = %#v, %v", manual, err)
	}
	claim, _ := store.ClaimDue(ctx, now.Add(time.Minute), 2*time.Minute)
	updated, _, err := store.FinishSuccess(ctx, *claim, now.Add(2*time.Minute))
	if err != nil || updated.NextRunAt == nil || !updated.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("manual success moved schedule: %#v, %v", updated, err)
	}
}

func TestScheduleStoreRecoversExpiredLeaseWithoutDuplicateOccurrence(t *testing.T) {
	state, _ := newTestStateDB(t)
	store, _ := NewScheduleStore(state)
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	created, _ := store.Create(ctx, testEverySchedule("恢复执行", time.Hour), now)
	first, _ := store.ClaimDue(ctx, now.Add(time.Hour), time.Minute)
	recovered, err := store.ClaimDue(ctx, now.Add(time.Hour+time.Minute), time.Minute)
	if err != nil || recovered == nil || recovered.Run.ID != first.Run.ID || recovered.Run.Attempt != 2 || recovered.Schedule.ID != created.ID {
		t.Fatalf("recovered claim = %#v, %v", recovered, err)
	}
	if _, _, err := store.FinishSuccess(ctx, *first, now.Add(time.Hour+2*time.Minute)); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("stale attempt completion error = %v", err)
	}
	if renewed, err := store.RenewLease(
		ctx, first.Run.ID, first.Run.Attempt, now.Add(time.Hour+4*time.Minute), now.Add(time.Hour+2*time.Minute),
	); err != nil || renewed {
		t.Fatalf("stale attempt renewal = %t, %v", renewed, err)
	}
	if err := store.SetRunRequestMessage(
		ctx, first.Run.ID, first.Run.Attempt, "msg_stale", now.Add(time.Hour+2*time.Minute),
	); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("stale attempt recorder error = %v", err)
	}
	if _, _, err := store.FinishSuccess(ctx, *recovered, now.Add(time.Hour+3*time.Minute)); err != nil {
		t.Fatalf("recovered completion error = %v", err)
	}
}

func TestScheduleStoreRejectsUnsafeDisplayText(t *testing.T) {
	state, _ := newTestStateDB(t)
	schedules, _ := NewScheduleStore(state)
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	input := testEverySchedule("多行\n名称", time.Hour)
	if _, err := schedules.Create(t.Context(), input, now); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("multiline name error = %v", err)
	}
	created, err := schedules.Create(t.Context(), testEverySchedule("安全名称", time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schedules.Pause(t.Context(), created.ID, created.Revision, strings.Repeat("理", MaxSchedulePauseReasonBytes), now); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("oversized pause reason error = %v", err)
	}
}
