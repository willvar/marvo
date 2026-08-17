package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"marvo/internal/scheduling"
)

const (
	ScheduleStatusActive    = "active"
	ScheduleStatusPaused    = "paused"
	ScheduleStatusCompleted = "completed"

	ScheduleRunQueued       = "queued"
	ScheduleRunRunning      = "running"
	ScheduleRunWaitingRetry = "waiting_retry"
	ScheduleRunSucceeded    = "succeeded"
	ScheduleRunFailed       = "failed"
	ScheduleRunTimedOut     = "timed_out"
	ScheduleRunCancelled    = "cancelled"

	ScheduleTriggerScheduled = "scheduled"
	ScheduleTriggerManual    = "manual"

	MaxScheduleNameRunes        = 200
	MaxScheduleInstructionBytes = 64 << 10
	MaxSchedulePauseReasonBytes = 1000
	MaxScheduleErrorBytes       = 4096
	MaxScheduleRunsPerTask      = 1000
)

var (
	ErrInvalidSchedule  = errors.New("invalid schedule")
	ErrScheduleNotFound = errors.New("schedule not found")
	ErrScheduleConflict = errors.New("schedule has changed")
	ErrScheduleBusy     = errors.New("schedule is already running")
)

type Schedule struct {
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	Instruction         string                `json:"instruction"`
	Definition          scheduling.Definition `json:"schedule"`
	Status              string                `json:"status"`
	NextRunAt           *time.Time            `json:"next_run_at"`
	SessionID           string                `json:"session_id,omitempty"`
	Revision            int64                 `json:"revision"`
	ConsecutiveFailures int                   `json:"consecutive_failures"`
	LastError           string                `json:"last_error,omitempty"`
	LastRunAt           *time.Time            `json:"last_run_at"`
	PausedReason        string                `json:"paused_reason,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

type ScheduleInput struct {
	Name        string                `json:"name"`
	Instruction string                `json:"instruction"`
	Definition  scheduling.Definition `json:"schedule"`
}

type ScheduleRun struct {
	ID                  string     `json:"id"`
	ScheduleID          string     `json:"schedule_id"`
	ScheduleRevision    int64      `json:"schedule_revision"`
	Trigger             string     `json:"trigger"`
	ScheduledFor        time.Time  `json:"scheduled_for"`
	Status              string     `json:"status"`
	Attempt             int        `json:"attempt"`
	NextAttemptAt       *time.Time `json:"next_attempt_at,omitempty"`
	LeaseUntil          *time.Time `json:"-"`
	SessionID           string     `json:"session_id,omitempty"`
	RequestMessageID    string     `json:"-"`
	MessageID           string     `json:"message_id,omitempty"`
	AdaptiveNextSeconds int64      `json:"-"`
	Error               string     `json:"error,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ClaimedScheduleRun struct {
	Schedule Schedule    `json:"schedule"`
	Run      ScheduleRun `json:"run"`
}

type ScheduleStore struct {
	state *StateDB
}

func NewScheduleStore(state *StateDB) (*ScheduleStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	return &ScheduleStore{state: state}, nil
}

func (s *ScheduleStore) Create(ctx context.Context, input ScheduleInput, now time.Time) (Schedule, error) {
	input, next, err := normalizeScheduleInput(input, now)
	if err != nil {
		return Schedule{}, err
	}
	id, err := randomID()
	if err != nil {
		return Schedule{}, err
	}
	now = now.UTC().Truncate(time.Millisecond)
	specJSON, _ := json.Marshal(input.Definition.Spec)
	_, err = s.state.sql.ExecContext(ctx, `
		INSERT INTO schedules(
			id, name, instruction, schedule_kind, schedule_spec_json, timezone,
			status, next_run_at, revision, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, 'active', ?, 1, ?, ?)
	`, id, input.Name, input.Instruction, input.Definition.Kind, string(specJSON), input.Definition.Timezone,
		next.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Schedule{}, fmt.Errorf("create schedule: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *ScheduleStore) Get(ctx context.Context, id string) (Schedule, error) {
	if !validActivityID(id) {
		return Schedule{}, ErrScheduleNotFound
	}
	return scanSchedule(s.state.sql.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, id))
}

func (s *ScheduleStore) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.state.sql.QueryContext(ctx, scheduleSelect+`
		ORDER BY
			CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END,
			CASE WHEN next_run_at IS NULL THEN 1 ELSE 0 END,
			next_run_at, updated_at DESC, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Schedule, 0)
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *ScheduleStore) Update(ctx context.Context, id string, expectedRevision int64, input ScheduleInput, now time.Time) (Schedule, error) {
	if !validActivityID(id) || expectedRevision < 1 {
		return Schedule{}, ErrInvalidSchedule
	}
	input, next, err := normalizeScheduleInput(input, now)
	if err != nil {
		return Schedule{}, err
	}
	specJSON, _ := json.Marshal(input.Definition.Spec)
	now = now.UTC().Truncate(time.Millisecond)
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedules
		SET name = ?, instruction = ?, schedule_kind = ?, schedule_spec_json = ?, timezone = ?,
			next_run_at = CASE WHEN status = 'paused' THEN NULL ELSE ? END,
			status = CASE WHEN status = 'completed' THEN 'active' ELSE status END,
			revision = revision + 1, consecutive_failures = 0, last_error = '', paused_reason = '', updated_at = ?
		WHERE id = ? AND revision = ?
	`, input.Name, input.Instruction, input.Definition.Kind, string(specJSON), input.Definition.Timezone,
		next.UnixMilli(), now.UnixMilli(), id, expectedRevision)
	if err != nil {
		return Schedule{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrScheduleNotFound) {
			return Schedule{}, ErrScheduleNotFound
		}
		return Schedule{}, ErrScheduleConflict
	}
	return s.Get(ctx, id)
}

func (s *ScheduleStore) Pause(ctx context.Context, id string, expectedRevision int64, reason string, now time.Time) (Schedule, error) {
	reason = strings.TrimSpace(reason)
	if !utf8.ValidString(reason) || len(reason) > MaxSchedulePauseReasonBytes || containsDisallowedControl(reason, false) {
		return Schedule{}, ErrInvalidSchedule
	}
	return s.changeStatus(ctx, id, expectedRevision, ScheduleStatusPaused, reason, nil, now)
}

func (s *ScheduleStore) Resume(ctx context.Context, id string, expectedRevision int64, now time.Time) (Schedule, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Schedule{}, err
	}
	if current.Revision != expectedRevision {
		return Schedule{}, ErrScheduleConflict
	}
	var next time.Time
	if current.Definition.Kind == scheduling.KindAt && current.Definition.Spec.At != nil &&
		!current.Definition.Spec.At.After(now) {
		// A paused one-shot may already be overdue (for example after exhausting
		// retries). Continuing it means retrying now, not forcing the user to edit
		// the original timestamp first.
		next = now.UTC().Truncate(time.Millisecond)
	} else {
		next, err = scheduling.First(current.Definition, now)
		if err != nil {
			return Schedule{}, ErrInvalidSchedule
		}
	}
	return s.changeStatus(ctx, id, expectedRevision, ScheduleStatusActive, "", &next, now)
}

func (s *ScheduleStore) Complete(ctx context.Context, id string, expectedRevision int64, now time.Time) (Schedule, error) {
	return s.changeStatus(ctx, id, expectedRevision, ScheduleStatusCompleted, "", nil, now)
}

func (s *ScheduleStore) changeStatus(
	ctx context.Context,
	id string,
	expectedRevision int64,
	status string,
	reason string,
	next *time.Time,
	now time.Time,
) (Schedule, error) {
	if !validActivityID(id) || expectedRevision < 1 {
		return Schedule{}, ErrInvalidSchedule
	}
	var nextValue any
	if next != nil {
		nextValue = next.UTC().UnixMilli()
	}
	now = now.UTC().Truncate(time.Millisecond)
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedules
		SET status = ?, next_run_at = CASE WHEN ? = 'active' THEN ? ELSE NULL END,
			paused_reason = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?
	`, status, status, nextValue, reason, now.UnixMilli(), id, expectedRevision)
	if err != nil {
		return Schedule{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		if _, err := s.Get(ctx, id); errors.Is(err, ErrScheduleNotFound) {
			return Schedule{}, ErrScheduleNotFound
		}
		return Schedule{}, ErrScheduleConflict
	}
	return s.Get(ctx, id)
}

func (s *ScheduleStore) Delete(ctx context.Context, id string, expectedRevision int64) (bool, error) {
	if !validActivityID(id) || expectedRevision < 1 {
		return false, ErrInvalidSchedule
	}
	result, err := s.state.sql.ExecContext(ctx, `
		DELETE FROM schedules WHERE id = ? AND revision = ? AND NOT EXISTS (
			SELECT 1 FROM schedule_runs WHERE schedule_id = ? AND status IN ('queued', 'running', 'waiting_retry')
		)
	`, id, expectedRevision, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected > 0 {
		return affected > 0, err
	}
	if current, err := s.Get(ctx, id); err == nil {
		if current.Revision != expectedRevision {
			return false, ErrScheduleConflict
		}
		return false, ErrScheduleBusy
	} else if !errors.Is(err, ErrScheduleNotFound) {
		return false, err
	}
	return false, nil
}

func (s *ScheduleStore) RunNow(ctx context.Context, id string, now time.Time) (ScheduleRun, error) {
	if !validActivityID(id) {
		return ScheduleRun{}, ErrScheduleNotFound
	}
	now = now.UTC().Truncate(time.Millisecond)
	runID, err := randomID()
	if err != nil {
		return ScheduleRun{}, err
	}
	occurrenceID, err := randomID()
	if err != nil {
		return ScheduleRun{}, err
	}
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return ScheduleRun{}, err
	}
	defer tx.Rollback()
	schedule, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, id))
	if err != nil {
		return ScheduleRun{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schedule_runs(
			id, schedule_id, schedule_revision, occurrence_key, trigger_kind, scheduled_for,
			status, attempt, next_attempt_at, created_at, updated_at
		) VALUES(?, ?, ?, ?, 'manual', ?, 'queued', 0, ?, ?, ?)
	`, runID, id, schedule.Revision, "manual:"+occurrenceID, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if isUniqueConstraint(err) {
			return ScheduleRun{}, ErrScheduleBusy
		}
		return ScheduleRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ScheduleRun{}, err
	}
	return s.GetRun(ctx, runID)
}

func (s *ScheduleStore) CancelPendingRun(ctx context.Context, scheduleID, runID string, finishedAt time.Time) (bool, error) {
	if !validActivityID(scheduleID) || !validActivityID(runID) {
		return false, ErrInvalidSchedule
	}
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	run, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+`
		WHERE id = ? AND schedule_id = ? AND status IN ('queued', 'waiting_retry')
	`, runID, scheduleID))
	if errors.Is(err, ErrScheduleNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, scheduleID))
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs SET status = 'cancelled', next_attempt_at = NULL, lease_until = NULL,
			error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('queued', 'waiting_retry')
	`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), run.ID)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return false, nil
	}
	if current.Status == ScheduleStatusActive && current.Revision == run.ScheduleRevision && run.Trigger == ScheduleTriggerScheduled {
		next, nextErr := scheduling.Next(current.Definition, finishedAt, 0)
		if nextErr != nil {
			return false, nextErr
		}
		if next == nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE schedules SET status = 'completed', next_run_at = NULL, last_run_at = ?, updated_at = ? WHERE id = ?
			`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE schedules SET next_run_at = ?, last_run_at = ?, updated_at = ? WHERE id = ?
			`, next.UnixMilli(), finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		}
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE schedules SET last_run_at = ?, updated_at = ? WHERE id = ?
		`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
	}
	if err != nil {
		return false, err
	}
	if err := pruneScheduleRuns(ctx, tx, current.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ScheduleStore) GetRun(ctx context.Context, runID string) (ScheduleRun, error) {
	if !validActivityID(runID) {
		return ScheduleRun{}, ErrScheduleNotFound
	}
	return scanScheduleRun(s.state.sql.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, runID))
}

func (s *ScheduleStore) ListRuns(ctx context.Context, scheduleID string, limit int) ([]ScheduleRun, error) {
	if !validActivityID(scheduleID) {
		return nil, ErrInvalidSchedule
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := s.state.sql.QueryContext(ctx, scheduleRunSelect+`
		WHERE schedule_id = ? ORDER BY created_at DESC, id DESC LIMIT ?
	`, scheduleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ScheduleRun, 0, limit)
	for rows.Next() {
		run, err := scanScheduleRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (s *ScheduleStore) ListActiveRuns(ctx context.Context) (map[string]ScheduleRun, error) {
	rows, err := s.state.sql.QueryContext(ctx, scheduleRunSelect+`
		WHERE status IN ('queued', 'running', 'waiting_retry')
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ScheduleRun)
	for rows.Next() {
		run, err := scanScheduleRun(rows)
		if err != nil {
			return nil, err
		}
		result[run.ScheduleID] = run
	}
	return result, rows.Err()
}

// NextWake returns the earliest durable reason the in-memory scheduler should
// inspect this user again: a natural occurrence, retry/manual run, or expired lease.
func (s *ScheduleStore) NextWake(ctx context.Context) (*time.Time, error) {
	var millis sql.NullInt64
	err := s.state.sql.QueryRowContext(ctx, `
		SELECT MIN(due_at) FROM (
			SELECT next_run_at AS due_at FROM schedules s
			WHERE status = 'active' AND next_run_at IS NOT NULL
				AND NOT EXISTS (
					SELECT 1 FROM schedule_runs r WHERE r.schedule_id = s.id
					AND r.status IN ('queued', 'running', 'waiting_retry')
				)
			UNION ALL
			SELECT next_attempt_at AS due_at FROM schedule_runs
			WHERE status IN ('queued', 'waiting_retry') AND next_attempt_at IS NOT NULL
			UNION ALL
			SELECT lease_until AS due_at FROM schedule_runs
			WHERE status = 'running' AND lease_until IS NOT NULL
		)
	`).Scan(&millis)
	if err != nil || !millis.Valid {
		return nil, err
	}
	value := time.UnixMilli(millis.Int64).UTC()
	return &value, nil
}

func (s *ScheduleStore) ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*ClaimedScheduleRun, error) {
	if lease < time.Minute {
		return nil, ErrInvalidSchedule
	}
	now = now.UTC().Truncate(time.Millisecond)
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// An expired lease means Marvo disappeared while OpenCode was active. The
	// same durable run is retried; a second occurrence is never created.
	if _, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs
		SET status = 'waiting_retry', next_attempt_at = ?, lease_until = NULL,
			error = CASE WHEN error = '' THEN '执行在服务重启后恢复' ELSE error END, updated_at = ?
		WHERE status = 'running' AND lease_until IS NOT NULL AND lease_until <= ?
	`, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return nil, err
	}

	run, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+`
		WHERE status IN ('queued', 'waiting_retry') AND next_attempt_at <= ?
		ORDER BY next_attempt_at, created_at, id LIMIT 1
	`, now.UnixMilli()))
	if errors.Is(err, ErrScheduleNotFound) {
		schedule, scheduleErr := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+`
			WHERE status = 'active' AND next_run_at IS NOT NULL AND next_run_at <= ?
				AND NOT EXISTS (
					SELECT 1 FROM schedule_runs r WHERE r.schedule_id = schedules.id
					AND r.status IN ('queued', 'running', 'waiting_retry')
				)
			ORDER BY next_run_at, id LIMIT 1
		`, now.UnixMilli()))
		if errors.Is(scheduleErr, ErrScheduleNotFound) {
			return nil, nil
		}
		if scheduleErr != nil {
			return nil, scheduleErr
		}
		runID, idErr := randomID()
		if idErr != nil {
			return nil, idErr
		}
		scheduledFor := schedule.NextRunAt.UTC()
		occurrenceKey := fmt.Sprintf("scheduled:%s:%d", schedule.ID, scheduledFor.UnixMilli())
		_, insertErr := tx.ExecContext(ctx, `
			INSERT INTO schedule_runs(
				id, schedule_id, schedule_revision, occurrence_key, trigger_kind, scheduled_for,
				status, attempt, next_attempt_at, created_at, updated_at
			) VALUES(?, ?, ?, ?, 'scheduled', ?, 'queued', 0, ?, ?, ?)
		`, runID, schedule.ID, schedule.Revision, occurrenceKey, scheduledFor.UnixMilli(),
			now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
		if insertErr != nil {
			if isUniqueConstraint(insertErr) {
				return nil, nil
			}
			return nil, insertErr
		}
		run, err = scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, runID))
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(lease).Truncate(time.Millisecond)
	result, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs
		SET status = 'running', attempt = attempt + 1, next_attempt_at = NULL,
			lease_until = ?, started_at = COALESCE(started_at, ?), finished_at = NULL, updated_at = ?
		WHERE id = ? AND status IN ('queued', 'waiting_retry')
	`, leaseUntil.UnixMilli(), now.UnixMilli(), now.UnixMilli(), run.ID)
	if err != nil {
		return nil, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return nil, nil
	}
	claimedRun, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, run.ID))
	if err != nil {
		return nil, err
	}
	schedule, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, run.ScheduleID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ClaimedScheduleRun{Schedule: schedule, Run: claimedRun}, nil
}

func (s *ScheduleStore) RenewLease(ctx context.Context, runID string, expectedAttempt int, leaseUntil, now time.Time) (bool, error) {
	if !validActivityID(runID) || expectedAttempt < 1 {
		return false, ErrInvalidSchedule
	}
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedule_runs SET lease_until = ?, updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, leaseUntil.UTC().UnixMilli(), now.UTC().UnixMilli(), runID, expectedAttempt)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

func (s *ScheduleStore) SetRunSession(
	ctx context.Context,
	scheduleID, runID string,
	expectedAttempt int,
	sessionID string,
	now time.Time,
) error {
	sessionID = strings.TrimSpace(sessionID)
	if !validActivityID(scheduleID) || !validActivityID(runID) || expectedAttempt < 1 || sessionID == "" || len(sessionID) > 256 {
		return ErrInvalidSchedule
	}
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE schedules SET session_id = ?, updated_at = ? WHERE id = ?`, sessionID, now.UTC().UnixMilli(), scheduleID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs SET session_id = ?, updated_at = ?
		WHERE id = ? AND schedule_id = ? AND status = 'running' AND attempt = ?
	`, sessionID, now.UTC().UnixMilli(), runID, scheduleID, expectedAttempt)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return ErrScheduleNotFound
	}
	return tx.Commit()
}

func (s *ScheduleStore) SetRunRequestMessage(ctx context.Context, runID string, expectedAttempt int, messageID string, now time.Time) error {
	messageID = strings.TrimSpace(messageID)
	if !validActivityID(runID) || expectedAttempt < 1 || messageID == "" || len(messageID) > 256 {
		return ErrInvalidSchedule
	}
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedule_runs SET request_message_id = ?, message_id = '', updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, messageID, now.UTC().UnixMilli(), runID, expectedAttempt)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *ScheduleStore) SetRunResponseMessage(ctx context.Context, runID string, expectedAttempt int, messageID string, now time.Time) error {
	messageID = strings.TrimSpace(messageID)
	if !validActivityID(runID) || expectedAttempt < 1 || messageID == "" || len(messageID) > 256 {
		return ErrInvalidSchedule
	}
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedule_runs SET message_id = ?, updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, messageID, now.UTC().UnixMilli(), runID, expectedAttempt)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return ErrScheduleNotFound
	}
	return nil
}

func (s *ScheduleStore) SetAdaptiveNext(ctx context.Context, scheduleID string, delay time.Duration, now time.Time) (Schedule, error) {
	current, err := s.Get(ctx, scheduleID)
	if err != nil {
		return Schedule{}, err
	}
	delay, err = scheduling.ClampAdaptive(current.Definition, delay)
	if err != nil {
		return Schedule{}, ErrInvalidSchedule
	}
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedule_runs SET adaptive_next_seconds = ?, updated_at = ?
		WHERE schedule_id = ? AND status = 'running'
	`, int64(delay/time.Second), now.UTC().UnixMilli(), scheduleID)
	if err != nil {
		return Schedule{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return Schedule{}, ErrScheduleBusy
	}
	return current, nil
}

func (s *ScheduleStore) FinishSuccess(ctx context.Context, claimed ClaimedScheduleRun, finishedAt time.Time) (Schedule, ScheduleRun, error) {
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	defer tx.Rollback()
	run, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, claimed.Run.ID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	current, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, run.ScheduleID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if run.Status != ScheduleRunRunning || run.Attempt != claimed.Run.Attempt {
		return current, run, ErrScheduleConflict
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs SET status = 'succeeded', lease_until = NULL, next_attempt_at = NULL,
			error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), run.ID, claimed.Run.Attempt); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if current.Status == ScheduleStatusActive && current.Revision == run.ScheduleRevision && run.Trigger == ScheduleTriggerScheduled {
		proposal := time.Duration(run.AdaptiveNextSeconds) * time.Second
		next, nextErr := scheduling.Next(current.Definition, finishedAt, proposal)
		if nextErr != nil {
			return Schedule{}, ScheduleRun{}, nextErr
		}
		if next == nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE schedules SET status = 'completed', next_run_at = NULL, consecutive_failures = 0,
					last_error = '', paused_reason = '', last_run_at = ?, updated_at = ? WHERE id = ?
			`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE schedules SET next_run_at = ?, consecutive_failures = 0, last_error = '', paused_reason = '',
					last_run_at = ?, updated_at = ? WHERE id = ?
			`, next.UnixMilli(), finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		}
	} else if current.Revision == run.ScheduleRevision {
		_, err = tx.ExecContext(ctx, `
			UPDATE schedules SET consecutive_failures = 0, last_error = '',
				paused_reason = CASE WHEN paused_reason = 'failure' THEN '' ELSE paused_reason END,
				last_run_at = ?, updated_at = ? WHERE id = ?
		`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE schedules SET last_run_at = ?, updated_at = ? WHERE id = ?
		`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
	}
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if err := pruneScheduleRuns(ctx, tx, current.ID); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	updated, err := s.Get(ctx, current.ID)
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	finished, err := s.GetRun(ctx, run.ID)
	return updated, finished, err
}

type ScheduleFailure struct {
	Error       string
	Retryable   bool
	TimedOut    bool
	RetryDelay  time.Duration
	MaxAttempts int
}

func (s *ScheduleStore) FinishFailure(
	ctx context.Context,
	claimed ClaimedScheduleRun,
	failure ScheduleFailure,
	finishedAt time.Time,
) (Schedule, ScheduleRun, bool, error) {
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	failure.Error = normalizeScheduleError(failure.Error)
	if failure.MaxAttempts < 1 {
		failure.MaxAttempts = 1
	}
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	defer tx.Rollback()
	run, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, claimed.Run.ID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	current, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, run.ScheduleID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	if run.Status != ScheduleRunRunning || run.Attempt != claimed.Run.Attempt {
		return current, run, false, ErrScheduleConflict
	}
	canRetry := failure.Retryable && run.Attempt < failure.MaxAttempts && current.Status == ScheduleStatusActive && current.Revision == run.ScheduleRevision
	if canRetry {
		retryAt := finishedAt.Add(failure.RetryDelay).Truncate(time.Millisecond)
		_, err = tx.ExecContext(ctx, `
			UPDATE schedule_runs SET status = 'waiting_retry', next_attempt_at = ?, lease_until = NULL,
				error = ?, finished_at = NULL, updated_at = ?
			WHERE id = ? AND status = 'running' AND attempt = ?
		`, retryAt.UnixMilli(), failure.Error, finishedAt.UnixMilli(), run.ID, claimed.Run.Attempt)
		if err == nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE schedules SET consecutive_failures = consecutive_failures + 1, last_error = ?,
					last_run_at = ?, updated_at = ? WHERE id = ?
			`, failure.Error, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		}
	} else {
		terminal := ScheduleRunFailed
		if failure.TimedOut {
			terminal = ScheduleRunTimedOut
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE schedule_runs SET status = ?, next_attempt_at = NULL, lease_until = NULL,
				error = ?, finished_at = ?, updated_at = ?
			WHERE id = ? AND status = 'running' AND attempt = ?
		`, terminal, failure.Error, finishedAt.UnixMilli(), finishedAt.UnixMilli(), run.ID, claimed.Run.Attempt)
		if err == nil {
			if current.Status == ScheduleStatusActive && current.Revision == run.ScheduleRevision && run.Trigger == ScheduleTriggerScheduled {
				_, err = tx.ExecContext(ctx, `
					UPDATE schedules SET status = 'paused', consecutive_failures = consecutive_failures + 1,
						last_error = ?, paused_reason = 'failure', last_run_at = ?, updated_at = ? WHERE id = ?
				`, failure.Error, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
			} else if current.Revision == run.ScheduleRevision {
				_, err = tx.ExecContext(ctx, `
					UPDATE schedules SET consecutive_failures = consecutive_failures + 1, last_error = ?,
						last_run_at = ?, updated_at = ? WHERE id = ?
				`, failure.Error, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
			} else {
				_, err = tx.ExecContext(ctx, `
					UPDATE schedules SET last_run_at = ?, updated_at = ? WHERE id = ?
				`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
			}
		}
	}
	if err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	if !canRetry {
		if err := pruneScheduleRuns(ctx, tx, current.ID); err != nil {
			return Schedule{}, ScheduleRun{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	updated, err := s.Get(ctx, current.ID)
	if err != nil {
		return Schedule{}, ScheduleRun{}, false, err
	}
	updatedRun, err := s.GetRun(ctx, run.ID)
	return updated, updatedRun, canRetry, err
}

func (s *ScheduleStore) FinishCancelled(ctx context.Context, claimed ClaimedScheduleRun, finishedAt time.Time) (Schedule, ScheduleRun, error) {
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	tx, err := s.state.sql.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	defer tx.Rollback()
	run, err := scanScheduleRun(tx.QueryRowContext(ctx, scheduleRunSelect+` WHERE id = ?`, claimed.Run.ID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	current, err := scanSchedule(tx.QueryRowContext(ctx, scheduleSelect+` WHERE id = ?`, run.ScheduleID))
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if run.Status != ScheduleRunRunning || run.Attempt != claimed.Run.Attempt {
		return current, run, ErrScheduleConflict
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE schedule_runs SET status = 'cancelled', lease_until = NULL, next_attempt_at = NULL,
			error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), run.ID, claimed.Run.Attempt); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if current.Status == ScheduleStatusActive && current.Revision == run.ScheduleRevision && run.Trigger == ScheduleTriggerScheduled {
		next, nextErr := scheduling.Next(current.Definition, finishedAt, 0)
		if nextErr != nil {
			return Schedule{}, ScheduleRun{}, nextErr
		}
		if next == nil {
			_, err = tx.ExecContext(ctx, `UPDATE schedules SET status = 'completed', next_run_at = NULL, last_run_at = ?, updated_at = ? WHERE id = ?`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE schedules SET next_run_at = ?, last_run_at = ?, updated_at = ? WHERE id = ?`, next.UnixMilli(), finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, updated_at = ? WHERE id = ?`, finishedAt.UnixMilli(), finishedAt.UnixMilli(), current.ID)
	}
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if err := pruneScheduleRuns(ctx, tx, current.ID); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	updated, err := s.Get(ctx, current.ID)
	if err != nil {
		return Schedule{}, ScheduleRun{}, err
	}
	updatedRun, err := s.GetRun(ctx, run.ID)
	return updated, updatedRun, err
}

func (s *ScheduleStore) InterruptRun(ctx context.Context, runID string, expectedAttempt int, retryAt time.Time, reason string) error {
	if !validActivityID(runID) || expectedAttempt < 1 {
		return ErrInvalidSchedule
	}
	reason = normalizeScheduleError(reason)
	now := time.Now().UTC().Truncate(time.Millisecond)
	result, err := s.state.sql.ExecContext(ctx, `
		UPDATE schedule_runs
		SET status = 'waiting_retry', next_attempt_at = ?, lease_until = NULL,
			error = ?, finished_at = NULL, updated_at = ?
		WHERE id = ? AND status = 'running' AND attempt = ?
	`, retryAt.UTC().UnixMilli(), reason, now.UnixMilli(), runID, expectedAttempt)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrScheduleConflict
	}
	return nil
}

func normalizeScheduleInput(input ScheduleInput, now time.Time) (ScheduleInput, time.Time, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if !utf8.ValidString(input.Name) || !utf8.ValidString(input.Instruction) || input.Name == "" || input.Instruction == "" ||
		utf8.RuneCountInString(input.Name) > MaxScheduleNameRunes || len(input.Instruction) > MaxScheduleInstructionBytes {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	if containsDisallowedControl(input.Name, false) || containsDisallowedControl(input.Instruction, true) {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	definition, err := scheduling.Normalize(input.Definition, now)
	if err != nil {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	input.Definition = definition
	next, err := scheduling.First(definition, now)
	if err != nil {
		return ScheduleInput{}, time.Time{}, ErrInvalidSchedule
	}
	return input, next, nil
}

func containsDisallowedControl(value string, multiline bool) bool {
	for _, character := range value {
		if unicode.IsControl(character) && (!multiline || (character != '\n' && character != '\t')) {
			return true
		}
	}
	return false
}

func normalizeScheduleError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "智能体执行失败"
	}
	if len(value) <= MaxScheduleErrorBytes {
		return value
	}
	value = value[:MaxScheduleErrorBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func pruneScheduleRuns(ctx context.Context, tx *sql.Tx, scheduleID string) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM schedule_runs WHERE schedule_id = ? AND status NOT IN ('queued', 'running', 'waiting_retry')
			AND id NOT IN (
				SELECT id FROM schedule_runs WHERE schedule_id = ? AND status NOT IN ('queued', 'running', 'waiting_retry')
				ORDER BY created_at DESC, id DESC LIMIT ?
			)
	`, scheduleID, scheduleID, MaxScheduleRunsPerTask)
	return err
}

const scheduleSelect = `
	SELECT id, name, instruction, schedule_kind, schedule_spec_json, timezone, status,
		next_run_at, session_id, revision, consecutive_failures, last_error, last_run_at,
		paused_reason, created_at, updated_at
	FROM schedules
`

func scanSchedule(scanner interface{ Scan(...any) error }) (Schedule, error) {
	var item Schedule
	var kind string
	var specJSON string
	var nextRunAt sql.NullInt64
	var lastRunAt sql.NullInt64
	var createdAt int64
	var updatedAt int64
	if err := scanner.Scan(
		&item.ID, &item.Name, &item.Instruction, &kind, &specJSON, &item.Definition.Timezone,
		&item.Status, &nextRunAt, &item.SessionID, &item.Revision, &item.ConsecutiveFailures,
		&item.LastError, &lastRunAt, &item.PausedReason, &createdAt, &updatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrScheduleNotFound
	} else if err != nil {
		return Schedule{}, err
	}
	item.Definition.Kind = scheduling.Kind(kind)
	if err := json.Unmarshal([]byte(specJSON), &item.Definition.Spec); err != nil {
		return Schedule{}, fmt.Errorf("decode schedule definition: %w", err)
	}
	if _, err := scheduling.Next(item.Definition, time.Now(), 0); err != nil {
		return Schedule{}, fmt.Errorf("stored schedule is invalid: %w", err)
	}
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	item.NextRunAt = nullableMillis(nextRunAt)
	item.LastRunAt = nullableMillis(lastRunAt)
	return item, nil
}

const scheduleRunSelect = `
	SELECT id, schedule_id, schedule_revision, trigger_kind, scheduled_for, status, attempt,
		next_attempt_at, lease_until, session_id, request_message_id, message_id, adaptive_next_seconds, error,
		created_at, started_at, finished_at, updated_at
	FROM schedule_runs
`

func scanScheduleRun(scanner interface{ Scan(...any) error }) (ScheduleRun, error) {
	var item ScheduleRun
	var scheduledFor int64
	var nextAttemptAt sql.NullInt64
	var leaseUntil sql.NullInt64
	var createdAt int64
	var startedAt sql.NullInt64
	var finishedAt sql.NullInt64
	var updatedAt int64
	if err := scanner.Scan(
		&item.ID, &item.ScheduleID, &item.ScheduleRevision, &item.Trigger, &scheduledFor,
		&item.Status, &item.Attempt, &nextAttemptAt, &leaseUntil, &item.SessionID, &item.RequestMessageID, &item.MessageID,
		&item.AdaptiveNextSeconds, &item.Error, &createdAt, &startedAt, &finishedAt, &updatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return ScheduleRun{}, ErrScheduleNotFound
	} else if err != nil {
		return ScheduleRun{}, err
	}
	item.ScheduledFor = time.UnixMilli(scheduledFor).UTC()
	item.NextAttemptAt = nullableMillis(nextAttemptAt)
	item.LeaseUntil = nullableMillis(leaseUntil)
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.StartedAt = nullableMillis(startedAt)
	item.FinishedAt = nullableMillis(finishedAt)
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return item, nil
}

func nullableMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
