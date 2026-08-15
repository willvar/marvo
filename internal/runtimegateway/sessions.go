package runtimegateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	defaultSessionSearchLimit = 20
	defaultSessionReadLimit   = 50
	maxSessionToolLimit       = 100
	maxSessionQueryRunes      = 200
	maxSessionUpstreamBody    = 8 << 20
	maxSessionResultText      = 256 << 10
	maxSessionPartText        = 64 << 10
	maxSessionAttachments     = 100
	maxSessionTitleBytes      = 512
	maxSessionFilenameBytes   = 512
)

var validSessionID = regexp.MustCompile(`^ses_[A-Za-z0-9]+$`)

type sessionsToolInput struct {
	Action    string  `json:"action"`
	Query     *string `json:"query"`
	SessionID *string `json:"session_id"`
	Limit     *int    `json:"limit"`
}

type openCodeSession struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	ParentID  string `json:"parentID"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type safeSessionSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type openCodeSessionMessage struct {
	Info struct {
		Role string `json:"role"`
		Time struct {
			Created int64 `json:"created"`
		} `json:"time"`
	} `json:"info"`
	Parts []struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Filename  string `json:"filename"`
		Synthetic bool   `json:"synthetic"`
		Ignored   bool   `json:"ignored"`
	} `json:"parts"`
}

type safeSessionMessage struct {
	Role        string   `json:"role"`
	CreatedAt   int64    `json:"created_at"`
	Text        string   `json:"text,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
}

var errOpenCodeSessionNotFound = errors.New("OpenCode session not found")

func (s *Server) handleSessionsTool(w http.ResponseWriter, r *http.Request) {
	var input sessionsToolInput
	if err := decodeToolJSON(r, &input); err != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid sessions operation"})
		return
	}
	switch input.Action {
	case "search":
		s.handleSessionSearch(input, w, r)
	case "read":
		s.handleSessionRead(input, w, r)
	default:
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported sessions operation"})
	}
}

func (s *Server) handleSessionSearch(input sessionsToolInput, w http.ResponseWriter, r *http.Request) {
	if input.SessionID != nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "search does not accept session_id"})
		return
	}
	query := ""
	if input.Query != nil {
		query = strings.TrimSpace(*input.Query)
		if utf8.RuneCountInString(query) > maxSessionQueryRunes {
			writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "session query is too long"})
			return
		}
	}
	limit, ok := sessionToolLimit(input.Limit, defaultSessionSearchLimit, maxSessionToolLimit)
	if !ok {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session search limit"})
		return
	}
	target, release, ok := s.sessionRuntime(w, r)
	if !ok {
		return
	}
	defer release()
	parameters := url.Values{
		"directory": {"/workspace"},
		"scope":     {"project"},
		"roots":     {"true"},
		"limit":     {fmt.Sprintf("%d", limit)},
	}
	if query != "" {
		parameters.Set("search", query)
	}
	var upstream []openCodeSession
	if err := s.readOpenCodeSessionJSON(r.Context(), target, "/session", parameters, &upstream); err != nil {
		s.writeSessionUpstreamError(w, r.PathValue("userID"), err)
		return
	}
	queryFolded := strings.ToLower(query)
	sessions := make([]safeSessionSummary, 0, min(limit, len(upstream)))
	for _, session := range upstream {
		if len(session.ID) > 128 || !validSessionID.MatchString(session.ID) ||
			session.Directory != "/workspace" || session.ParentID != "" {
			continue
		}
		if queryFolded != "" && !strings.Contains(strings.ToLower(session.Title), queryFolded) {
			continue
		}
		title, _ := truncateSessionText(strings.TrimSpace(session.Title), maxSessionTitleBytes)
		sessions = append(sessions, safeSessionSummary{
			ID: session.ID, Title: title, CreatedAt: session.Time.Created, UpdatedAt: session.Time.Updated,
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].UpdatedAt > sessions[j].UpdatedAt })
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleSessionRead(input sessionsToolInput, w http.ResponseWriter, r *http.Request) {
	if input.Query != nil || input.SessionID == nil {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "read requires only session_id and optional limit"})
		return
	}
	sessionID := strings.TrimSpace(*input.SessionID)
	if len(sessionID) > 128 || !validSessionID.MatchString(sessionID) {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session_id"})
		return
	}
	limit, ok := sessionToolLimit(input.Limit, defaultSessionReadLimit, maxSessionToolLimit)
	if !ok {
		writeGatewayJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session read limit"})
		return
	}
	target, release, ok := s.sessionRuntime(w, r)
	if !ok {
		return
	}
	defer release()
	parameters := url.Values{"directory": {"/workspace"}}
	var session openCodeSession
	if err := s.readOpenCodeSessionJSON(r.Context(), target, "/session/"+url.PathEscape(sessionID), parameters, &session); err != nil {
		s.writeSessionUpstreamError(w, r.PathValue("userID"), err)
		return
	}
	if session.ID != sessionID || session.Directory != "/workspace" {
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
		return
	}
	parameters.Set("limit", fmt.Sprintf("%d", limit))
	var upstream []openCodeSessionMessage
	path := "/session/" + url.PathEscape(sessionID) + "/message"
	if err := s.readOpenCodeSessionJSON(r.Context(), target, path, parameters, &upstream); err != nil {
		s.writeSessionUpstreamError(w, r.PathValue("userID"), err)
		return
	}
	messages, truncated := filterSessionMessages(upstream)
	title, titleTruncated := truncateSessionText(strings.TrimSpace(session.Title), maxSessionTitleBytes)
	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"session":   map[string]any{"id": session.ID, "title": title},
		"messages":  messages,
		"truncated": truncated || titleTruncated,
	})
}

func sessionToolLimit(value *int, fallback, maximum int) (int, bool) {
	if value == nil {
		return fallback, true
	}
	return *value, *value >= 1 && *value <= maximum
}

func (s *Server) sessionRuntime(w http.ResponseWriter, r *http.Request) (*RuntimeTarget, func(), bool) {
	userID := r.PathValue("userID")
	release := func() {}
	if tracker, ok := s.runtimes.(runtimeActivityTracker); ok {
		release = tracker.BeginUse(userID)
	}
	target, err := s.runtimes.Ensure(r.Context(), userID)
	if err != nil {
		release()
		slog.Error("runtime gateway: ensure Agent runtime for session tool failed", "user_id", userID, "error", err)
		writeGatewayJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Agent runtime unavailable"})
		return nil, nil, false
	}
	return target, release, true
}

func (s *Server) readOpenCodeSessionJSON(
	ctx context.Context,
	target *RuntimeTarget,
	path string,
	parameters url.Values,
	destination any,
) error {
	if target == nil || target.URL == nil {
		return errors.New("agent runtime has no URL")
	}
	endpoint := *target.URL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawPath = ""
	endpoint.RawQuery = parameters.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(target.Username, target.Password)
	response, err := s.sessions.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errOpenCodeSessionNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenCode session API returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSessionUpstreamBody+1))
	if err != nil {
		return err
	}
	if len(body) > maxSessionUpstreamBody {
		return errors.New("OpenCode session response is too large")
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return errors.New("OpenCode session response is invalid")
	}
	return nil
}

func (s *Server) writeSessionUpstreamError(w http.ResponseWriter, userID string, err error) {
	if errors.Is(err, errOpenCodeSessionNotFound) {
		writeGatewayJSON(w, http.StatusNotFound, map[string]any{"error": "session not found"})
		return
	}
	slog.Error("runtime gateway: read Agent session failed", "user_id", userID, "error", err)
	writeGatewayJSON(w, http.StatusBadGateway, map[string]any{"error": "Agent session history is unavailable"})
}

func filterSessionMessages(upstream []openCodeSessionMessage) ([]safeSessionMessage, bool) {
	result := make([]safeSessionMessage, 0, len(upstream))
	remaining := maxSessionResultText
	attachmentCount := 0
	truncated := false
	for _, message := range upstream {
		if message.Info.Role != "user" && message.Info.Role != "assistant" {
			continue
		}
		texts := make([]string, 0)
		attachments := make([]string, 0)
		for _, part := range message.Parts {
			switch part.Type {
			case "text":
				if part.Synthetic || part.Ignored || strings.TrimSpace(part.Text) == "" {
					continue
				}
				if remaining == 0 {
					truncated = true
					continue
				}
				text, partTruncated := truncateSessionText(strings.TrimSpace(part.Text), min(maxSessionPartText, remaining))
				if text != "" {
					texts = append(texts, text)
					remaining -= len(text)
				}
				truncated = truncated || partTruncated
			case "file":
				rawName := strings.TrimSpace(part.Filename)
				if rawName == "" {
					continue
				}
				if remaining == 0 || attachmentCount == maxSessionAttachments {
					truncated = true
					continue
				}
				name, nameTruncated := truncateSessionText(rawName, min(maxSessionFilenameBytes, remaining))
				if name != "" {
					attachments = append(attachments, name)
					attachmentCount++
					remaining -= len(name)
				}
				truncated = truncated || nameTruncated
			}
		}
		if len(texts) == 0 && len(attachments) == 0 {
			continue
		}
		result = append(result, safeSessionMessage{
			Role: message.Info.Role, CreatedAt: message.Info.Time.Created,
			Text: strings.Join(texts, "\n\n"), Attachments: attachments,
		})
	}
	return result, truncated
}

func truncateSessionText(value string, maximum int) (string, bool) {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximum {
		return value, false
	}
	end := maximum
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}
