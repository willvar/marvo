package handler

import (
	"errors"
	"log/slog"
	"marvo/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Leave room for JSON escaping without changing the 64 KiB decoded
// instruction limit enforced by the store.
const maxScheduleRequestBody = 512 << 10

type scheduleResponse struct {
	store.Schedule
	ActiveRun *store.ScheduleRun `json:"active_run,omitempty"`
}

type scheduleUpdateRequest struct {
	Revision int64 `json:"revision"`
	store.ScheduleInput
}

type scheduleRevisionRequest struct {
	Revision int64  `json:"revision"`
	Reason   string `json:"reason,omitempty"`
}

func (d *Dependencies) ListSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := d.Schedules.List(r.Context())
	if err != nil {
		d.scheduleInternalError(w, "list", err)
		return
	}
	active, err := d.Schedules.ListActiveRuns(r.Context())
	if err != nil {
		d.scheduleInternalError(w, "list active runs", err)
		return
	}
	result := make([]scheduleResponse, 0, len(items))
	for _, item := range items {
		response := scheduleResponse{Schedule: item}
		if run, ok := active[item.ID]; ok {
			copy := run
			response.ActiveRun = &copy
		}
		result = append(result, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": result})
}

func (d *Dependencies) GetSchedule(w http.ResponseWriter, r *http.Request) {
	item, err := d.Schedules.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	active, err := d.Schedules.ListActiveRuns(r.Context())
	if err != nil {
		d.scheduleInternalError(w, "load active run", err)
		return
	}
	response := scheduleResponse{Schedule: item}
	if run, ok := active[item.ID]; ok {
		copy := run
		response.ActiveRun = &copy
	}
	writeJSON(w, http.StatusOK, response)
}

func (d *Dependencies) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScheduleRequestBody)
	var input store.ScheduleInput
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务设置格式无效"})
		return
	}
	item, err := d.Schedules.Create(r.Context(), input, time.Now())
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	d.scheduleChanged()
	writeJSON(w, http.StatusCreated, scheduleResponse{Schedule: item})
}

func (d *Dependencies) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScheduleRequestBody)
	var body scheduleUpdateRequest
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务设置格式无效"})
		return
	}
	item, err := d.Schedules.Update(r.Context(), r.PathValue("id"), body.Revision, body.ScheduleInput, time.Now())
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	d.scheduleChanged()
	writeJSON(w, http.StatusOK, scheduleResponse{Schedule: item})
}

func (d *Dependencies) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body scheduleRevisionRequest
	if err := readJSON(r, &body); err != nil || body.Revision < 1 || body.Reason != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务版本无效"})
		return
	}
	deleted, err := d.Schedules.Delete(r.Context(), r.PathValue("id"), body.Revision)
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "自动任务不存在"})
		return
	}
	d.scheduleChanged()
	w.WriteHeader(http.StatusNoContent)
}

func (d *Dependencies) PauseSchedule(w http.ResponseWriter, r *http.Request) {
	d.changeScheduleStatus(w, r, "pause")
}

func (d *Dependencies) ResumeSchedule(w http.ResponseWriter, r *http.Request) {
	d.changeScheduleStatus(w, r, "resume")
}

func (d *Dependencies) CompleteSchedule(w http.ResponseWriter, r *http.Request) {
	d.changeScheduleStatus(w, r, "complete")
}

func (d *Dependencies) changeScheduleStatus(w http.ResponseWriter, r *http.Request, action string) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body scheduleRevisionRequest
	if err := readJSON(r, &body); err != nil || body.Revision < 1 || (action != "pause" && body.Reason != "") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务版本无效"})
		return
	}
	var item store.Schedule
	var err error
	switch action {
	case "pause":
		item, err = d.Schedules.Pause(r.Context(), r.PathValue("id"), body.Revision, body.Reason, time.Now())
	case "resume":
		item, err = d.Schedules.Resume(r.Context(), r.PathValue("id"), body.Revision, time.Now())
	case "complete":
		item, err = d.Schedules.Complete(r.Context(), r.PathValue("id"), body.Revision, time.Now())
	}
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	d.scheduleChanged()
	writeJSON(w, http.StatusOK, scheduleResponse{Schedule: item})
}

func (d *Dependencies) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	run, err := d.Schedules.RunNow(r.Context(), r.PathValue("id"), time.Now())
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	d.scheduleChanged()
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (d *Dependencies) StopScheduleRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		RunID string `json:"run_id"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.RunID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务运行标识无效"})
		return
	}
	stopped := d.Spaces != nil && d.Spaces.StopSchedule(d.UserID, r.PathValue("id"), body.RunID)
	if !stopped {
		var err error
		stopped, err = d.Schedules.CancelPendingRun(r.Context(), r.PathValue("id"), body.RunID, time.Now())
		if err != nil {
			d.writeScheduleError(w, err)
			return
		}
		if stopped {
			d.scheduleChanged()
		}
	}
	if !stopped {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "本轮运行已经发生变化，请刷新后重试"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"stopping": true})
}

func (d *Dependencies) ListScheduleRuns(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "运行记录数量无效"})
			return
		}
		limit = value
	}
	if _, err := d.Schedules.Get(r.Context(), r.PathValue("id")); err != nil {
		d.writeScheduleError(w, err)
		return
	}
	runs, err := d.Schedules.ListRuns(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		d.writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (d *Dependencies) scheduleChanged() {
	if d.Hub != nil {
		d.Hub.BroadcastAll(store.MustJSON(map[string]any{"action": "schedules_changed"}))
	}
	if d.Spaces != nil {
		d.Spaces.WakeSchedules(d.UserID)
	}
}

func (d *Dependencies) writeScheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidSchedule):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "自动任务设置无效"})
	case errors.Is(err, store.ErrScheduleNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "自动任务不存在"})
	case errors.Is(err, store.ErrScheduleConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "自动任务已经发生变化，请刷新后重试"})
	case errors.Is(err, store.ErrScheduleBusy):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "自动任务已有一次运行正在处理"})
	default:
		d.scheduleInternalError(w, "operation", err)
	}
}

func (d *Dependencies) scheduleInternalError(w http.ResponseWriter, operation string, err error) {
	slog.Error("automatic tasks: "+operation+" failed", "user_id", d.UserID, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "自动任务操作失败"})
}
