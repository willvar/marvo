package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const agentSessionRunGrace = 5 * time.Second

var errAgentGlobalPromptPending = errors.New("global prompt activation is waiting for active Agent runs")

type agentSessionRun struct {
	Reserved     time.Time
	ObservedBusy bool
}

type openCodeSessionStatus struct {
	Type string `json:"type"`
}

func (d *AgentDeps) activateSavedGlobalPrompt(ctx context.Context) (bool, error) {
	if d.globalPromptFile == nil || d.settingsStore == nil {
		return false, nil
	}
	d.promptMu.Lock()
	defer d.promptMu.Unlock()
	return d.syncGlobalPromptLocked(ctx)
}

func (d *AgentDeps) beginAgentPrompt(ctx context.Context, sessionID string) error {
	if sessionID == "" || d.globalPromptFile == nil || d.settingsStore == nil {
		return nil
	}
	d.promptMu.Lock()
	defer d.promptMu.Unlock()
	pending, err := d.syncGlobalPromptLocked(ctx)
	if err != nil {
		return err
	}
	if pending {
		return errAgentGlobalPromptPending
	}
	d.sessionRuns[sessionID] = agentSessionRun{Reserved: time.Now()}
	return nil
}

func (d *AgentDeps) releaseAgentPrompt(sessionID string) {
	if sessionID == "" {
		return
	}
	d.promptMu.Lock()
	delete(d.sessionRuns, sessionID)
	d.promptMu.Unlock()
}

func (d *AgentDeps) syncGlobalPromptLocked(ctx context.Context) (bool, error) {
	prompt := d.settingsStore.Get().GlobalPrompt
	matches, err := d.globalPromptFile.Matches(prompt)
	if err != nil {
		return false, err
	}
	if matches {
		return false, nil
	}
	statuses, err := d.openCodeSessionStatuses(ctx)
	if err != nil {
		return true, nil
	}
	if d.hasActiveAgentRunLocked(statuses) {
		return true, nil
	}
	if err := d.globalPromptFile.Sync(prompt); err != nil {
		return false, err
	}
	return false, nil
}

func (d *AgentDeps) hasActiveAgentRunLocked(statuses map[string]openCodeSessionStatus) bool {
	now := time.Now()
	for sessionID, run := range d.sessionRuns {
		status := statuses[sessionID]
		if isBusyAgentStatus(status.Type) {
			run.ObservedBusy = true
			d.sessionRuns[sessionID] = run
			continue
		}
		if run.ObservedBusy || now.Sub(run.Reserved) > agentSessionRunGrace {
			delete(d.sessionRuns, sessionID)
		}
	}
	if len(d.sessionRuns) > 0 {
		return true
	}
	for _, status := range statuses {
		if isBusyAgentStatus(status.Type) {
			return true
		}
	}
	return false
}

func isBusyAgentStatus(status string) bool {
	return status == "busy" || status == "retry"
}

func (d *AgentDeps) openCodeSessionStatuses(parent context.Context) (map[string]openCodeSessionStatus, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.openCodeURL+"/session/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("OpenCode /session/status returned %d", resp.StatusCode)
	}
	var statuses map[string]openCodeSessionStatus
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4<<20))
	if err := decoder.Decode(&statuses); err != nil {
		return nil, err
	}
	if statuses == nil {
		statuses = make(map[string]openCodeSessionStatus)
	}
	return statuses, nil
}
