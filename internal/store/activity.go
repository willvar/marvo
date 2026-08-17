package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ActivityKindNotice = "notice"
	ActivityKindChoice = "choice"

	MaxActivityTitleRunes   = 200
	MaxActivityContentBytes = 64 << 10
	MaxActivityChoices      = 20
	MaxActivityChoiceRunes  = 200
	MaxActivityReplyBytes   = 64 << 10
)

var (
	ErrInvalidActivity     = errors.New("invalid activity")
	ErrActivityNotFound    = errors.New("activity not found")
	ErrActivityResponded   = errors.New("activity already responded")
	ErrActivityUnavailable = errors.New("activity is unavailable")
)

type Activity struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	Choices         []string   `json:"choices"`
	Multiple        bool       `json:"multiple"`
	SourceSessionID string     `json:"-"`
	SourceMessageID string     `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	ReadAt          *time.Time `json:"read_at"`
	RespondedAt     *time.Time `json:"responded_at"`
	ResponseText    string     `json:"response_text,omitempty"`
	ResponseChoices []string   `json:"response_choices,omitempty"`
	ReplySessionID  string     `json:"reply_session_id,omitempty"`
	Replying        bool       `json:"replying"`
}

type ActivityPublish struct {
	Kind            string   `json:"kind"`
	Title           string   `json:"title"`
	Content         string   `json:"content"`
	Choices         []string `json:"choices,omitempty"`
	Multiple        bool     `json:"multiple,omitempty"`
	SourceSessionID string   `json:"source_session_id"`
	SourceMessageID string   `json:"source_message_id"`
}

type ActivityReply struct {
	Text      string   `json:"text"`
	Choices   []string `json:"choices,omitempty"`
	SessionID string   `json:"session_id"`
}

type ActivityPage struct {
	Activities []Activity `json:"activities"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Unread     int        `json:"unread"`
	Pending    int        `json:"pending"`
}

type activityCursor struct {
	CreatedAt int64  `json:"created_at"`
	ID        string `json:"id"`
}

type ActivityStore struct {
	state *StateDB
}

func NewActivityStore(state *StateDB) (*ActivityStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	return &ActivityStore{state: state}, nil
}

func (s *ActivityStore) Publish(input ActivityPublish) (Activity, bool, error) {
	normalized, err := normalizeActivityPublish(input)
	if err != nil {
		return Activity{}, false, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return Activity{}, false, err
	}
	digest := sha256.Sum256(payload)
	dedupeKey := hex.EncodeToString(digest[:])
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return Activity{}, false, err
	}
	defer tx.Rollback()
	if existing, err := scanActivity(tx.QueryRow(activitySelect+` WHERE dedupe_key = ?`, dedupeKey)); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrActivityNotFound) {
		return Activity{}, false, err
	}
	id, err := randomID()
	if err != nil {
		return Activity{}, false, err
	}
	activity := Activity{
		ID: id, Kind: normalized.Kind, Title: normalized.Title, Content: normalized.Content,
		Choices: normalized.Choices, Multiple: normalized.Multiple, SourceSessionID: normalized.SourceSessionID,
		SourceMessageID: normalized.SourceMessageID,
		CreatedAt:       time.Now().UTC(), ResponseChoices: []string{},
	}
	choicesJSON, _ := json.Marshal(activity.Choices)
	_, err = tx.Exec(`
		INSERT INTO activities(
			id, kind, title, content, choices_json, multiple,
			source_session_id, source_message_id, source_call_id, dedupe_key, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
	`, activity.ID, activity.Kind, activity.Title, activity.Content, string(choicesJSON),
		activity.Multiple, activity.SourceSessionID, activity.SourceMessageID, dedupeKey,
		activity.CreatedAt.UnixMilli())
	if err != nil {
		return Activity{}, false, fmt.Errorf("publish activity: %w", err)
	}
	rows, err := tx.Query(`SELECT id FROM connectors WHERE enabled = 1 ORDER BY created_at, id`)
	if err != nil {
		return Activity{}, false, fmt.Errorf("load Activity connectors: %w", err)
	}
	connectorIDs := make([]string, 0)
	for rows.Next() {
		var connectorID string
		if err := rows.Scan(&connectorID); err != nil {
			_ = rows.Close()
			return Activity{}, false, err
		}
		connectorIDs = append(connectorIDs, connectorID)
	}
	if err := rows.Close(); err != nil {
		return Activity{}, false, err
	}
	for _, connectorID := range connectorIDs {
		deliveryID, err := randomID()
		if err != nil {
			return Activity{}, false, err
		}
		if _, err := tx.Exec(`
			INSERT INTO activity_deliveries(
				id, activity_id, connector_id, status, attempt_count, next_attempt_at, created_at, updated_at
			) VALUES(?, ?, ?, 'pending', 0, ?, ?, ?)
		`, deliveryID, activity.ID, connectorID, activity.CreatedAt.UnixMilli(), activity.CreatedAt.UnixMilli(), activity.CreatedAt.UnixMilli()); err != nil {
			return Activity{}, false, fmt.Errorf("queue Activity delivery: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Activity{}, false, fmt.Errorf("commit Activity: %w", err)
	}
	return activity, true, nil
}

func (s *ActivityStore) Get(id string) (Activity, error) {
	if !validActivityID(id) {
		return Activity{}, ErrActivityNotFound
	}
	return scanActivity(s.state.sql.QueryRow(activitySelect+` WHERE id = ?`, id))
}

func (s *ActivityStore) HasSourceMessage(sessionID, messageID string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	messageID = strings.TrimSpace(messageID)
	if sessionID == "" || messageID == "" || len(sessionID) > 256 || len(messageID) > 256 {
		return false, ErrInvalidActivity
	}
	var exists int
	err := s.state.sql.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM activities WHERE source_session_id = ? AND source_message_id = ?
		)
	`, sessionID, messageID).Scan(&exists)
	return exists == 1, err
}

func (s *ActivityStore) List(limit int, cursor string) (ActivityPage, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	arguments := []any{}
	where := ""
	if cursor != "" {
		parsed, err := decodeActivityCursor(cursor)
		if err != nil {
			return ActivityPage{}, err
		}
		where = " WHERE created_at < ? OR (created_at = ? AND id < ?)"
		arguments = append(arguments, parsed.CreatedAt, parsed.CreatedAt, parsed.ID)
	}
	arguments = append(arguments, limit+1)
	rows, err := s.state.sql.Query(activitySelect+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, arguments...)
	if err != nil {
		return ActivityPage{}, err
	}
	defer rows.Close()
	activities := make([]Activity, 0, limit+1)
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return ActivityPage{}, err
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return ActivityPage{}, err
	}
	nextCursor := ""
	if len(activities) > limit {
		last := activities[limit-1]
		nextCursor = encodeActivityCursor(activityCursor{CreatedAt: last.CreatedAt.UnixMilli(), ID: last.ID})
		activities = activities[:limit]
	}
	unread, pending, err := s.Counts()
	if err != nil {
		return ActivityPage{}, err
	}
	return ActivityPage{Activities: activities, NextCursor: nextCursor, Unread: unread, Pending: pending}, nil
}

func (s *ActivityStore) Counts() (int, int, error) {
	var unread int
	var pending int
	if err := s.state.sql.QueryRow(`SELECT COUNT(*) FROM activities WHERE read_at IS NULL`).Scan(&unread); err != nil {
		return 0, 0, err
	}
	if err := s.state.sql.QueryRow(`
		SELECT COUNT(*) FROM activities
		WHERE kind = 'choice' AND responded_at IS NULL
	`).Scan(&pending); err != nil {
		return 0, 0, err
	}
	return unread, pending, nil
}

func (s *ActivityStore) MarkRead(ids []string) error {
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalidActivity
	}
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().UnixMilli()
	for _, id := range ids {
		if !validActivityID(id) {
			return ErrInvalidActivity
		}
		if _, err := tx.Exec(`UPDATE activities SET read_at = COALESCE(read_at, ?) WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ActivityStore) Delete(id string) (bool, error) {
	if !validActivityID(id) {
		return false, ErrInvalidActivity
	}
	result, err := s.state.sql.Exec(`DELETE FROM activities WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *ActivityStore) BeginReply(id string, reply ActivityReply) (Activity, error) {
	if !validActivityID(id) {
		return Activity{}, ErrActivityNotFound
	}
	reply.Text = strings.TrimSpace(reply.Text)
	reply.SessionID = strings.TrimSpace(reply.SessionID)
	if !utf8.ValidString(reply.Text) || len(reply.Text) > MaxActivityReplyBytes || reply.SessionID == "" || len(reply.SessionID) > 256 {
		return Activity{}, ErrInvalidActivity
	}
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return Activity{}, err
	}
	defer tx.Rollback()
	activity, err := scanActivity(tx.QueryRow(activitySelect+` WHERE id = ?`, id))
	if err != nil {
		return Activity{}, err
	}
	if activity.RespondedAt != nil || activity.Replying {
		return Activity{}, ErrActivityResponded
	}
	reply.Choices, err = normalizeActivityResponseChoices(activity, reply.Choices)
	if err != nil {
		return Activity{}, err
	}
	if reply.Text == "" && len(reply.Choices) == 0 {
		return Activity{}, ErrInvalidActivity
	}
	choicesJSON, _ := json.Marshal(reply.Choices)
	now := time.Now().UTC()
	result, err := tx.Exec(`
		UPDATE activities
		SET read_at = COALESCE(read_at, ?), response_text = ?, response_choices_json = ?,
			reply_session_id = ?, reply_reserved_at = ?
		WHERE id = ? AND responded_at IS NULL AND reply_session_id = ''
	`, now.UnixMilli(), reply.Text, string(choicesJSON), reply.SessionID, now.UnixMilli(), id)
	if err != nil {
		return Activity{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Activity{}, ErrActivityResponded
	}
	if err := tx.Commit(); err != nil {
		return Activity{}, err
	}
	activity.ReadAt = &now
	activity.ResponseText = reply.Text
	activity.ResponseChoices = reply.Choices
	activity.ReplySessionID = reply.SessionID
	activity.Replying = true
	return activity, nil
}

func (s *ActivityStore) CompleteReply(id, sessionID string) (Activity, error) {
	if !validActivityID(id) || strings.TrimSpace(sessionID) == "" {
		return Activity{}, ErrInvalidActivity
	}
	now := time.Now().UTC()
	result, err := s.state.sql.Exec(`
		UPDATE activities SET responded_at = ?, reply_reserved_at = NULL
		WHERE id = ? AND reply_session_id = ? AND responded_at IS NULL
	`, now.UnixMilli(), id, sessionID)
	if err != nil {
		return Activity{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Activity{}, ErrActivityUnavailable
	}
	return s.Get(id)
}

func (s *ActivityStore) CancelReply(id, sessionID string) error {
	if !validActivityID(id) || strings.TrimSpace(sessionID) == "" {
		return ErrInvalidActivity
	}
	_, err := s.state.sql.Exec(`
		UPDATE activities
		SET response_text = '', response_choices_json = '[]', reply_session_id = '', reply_reserved_at = NULL
		WHERE id = ? AND reply_session_id = ? AND responded_at IS NULL
	`, id, sessionID)
	return err
}

func (s *ActivityStore) DetachReplySession(sessionID string) (int64, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 256 || strings.ContainsAny(sessionID, "\r\n") {
		return 0, ErrInvalidActivity
	}
	result, err := s.state.sql.Exec(`
		UPDATE activities
		SET response_text = CASE WHEN responded_at IS NULL THEN '' ELSE response_text END,
			response_choices_json = CASE WHEN responded_at IS NULL THEN '[]' ELSE response_choices_json END,
			reply_session_id = '', reply_reserved_at = NULL
		WHERE reply_session_id = ?
	`, sessionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

const activitySelect = `
	SELECT id, kind, title, content, choices_json, multiple,
		source_session_id, source_message_id, created_at,
		read_at, responded_at, response_text, response_choices_json, reply_session_id, reply_reserved_at
	FROM activities`

func scanActivity(row rowScanner) (Activity, error) {
	var activity Activity
	var choicesJSON string
	var responseChoicesJSON string
	var createdAt int64
	var readAt sql.NullInt64
	var respondedAt sql.NullInt64
	var replyReservedAt sql.NullInt64
	err := row.Scan(
		&activity.ID, &activity.Kind, &activity.Title, &activity.Content, &choicesJSON, &activity.Multiple,
		&activity.SourceSessionID, &activity.SourceMessageID, &createdAt,
		&readAt, &respondedAt, &activity.ResponseText, &responseChoicesJSON, &activity.ReplySessionID,
		&replyReservedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Activity{}, ErrActivityNotFound
	}
	if err != nil {
		return Activity{}, err
	}
	if err := json.Unmarshal([]byte(choicesJSON), &activity.Choices); err != nil {
		return Activity{}, err
	}
	if err := json.Unmarshal([]byte(responseChoicesJSON), &activity.ResponseChoices); err != nil {
		return Activity{}, err
	}
	activity.CreatedAt = time.UnixMilli(createdAt).UTC()
	activity.ReadAt = optionalActivityTime(readAt)
	activity.RespondedAt = optionalActivityTime(respondedAt)
	activity.Replying = activity.RespondedAt == nil && replyReservedAt.Valid && activity.ReplySessionID != ""
	return activity, nil
}

func optionalActivityTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}

func normalizeActivityPublish(input ActivityPublish) (ActivityPublish, error) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.SourceMessageID = strings.TrimSpace(input.SourceMessageID)
	if input.Kind != ActivityKindNotice && input.Kind != ActivityKindChoice {
		return ActivityPublish{}, ErrInvalidActivity
	}
	if !utf8.ValidString(input.Title) || input.Title == "" || utf8.RuneCountInString(input.Title) > MaxActivityTitleRunes ||
		!utf8.ValidString(input.Content) || input.Content == "" || len(input.Content) > MaxActivityContentBytes {
		return ActivityPublish{}, ErrInvalidActivity
	}
	for _, value := range []string{input.SourceSessionID, input.SourceMessageID} {
		if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n") {
			return ActivityPublish{}, ErrInvalidActivity
		}
	}
	choices, err := normalizeActivityChoices(input.Choices)
	if err != nil {
		return ActivityPublish{}, err
	}
	if input.Kind == ActivityKindNotice && (len(choices) != 0 || input.Multiple) {
		return ActivityPublish{}, ErrInvalidActivity
	}
	if input.Kind == ActivityKindChoice && len(choices) < 2 {
		return ActivityPublish{}, ErrInvalidActivity
	}
	input.Choices = choices
	return input, nil
}

func normalizeActivityChoices(choices []string) ([]string, error) {
	if len(choices) > MaxActivityChoices {
		return nil, ErrInvalidActivity
	}
	result := make([]string, 0, len(choices))
	seen := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		choice = strings.TrimSpace(choice)
		if !utf8.ValidString(choice) || choice == "" || utf8.RuneCountInString(choice) > MaxActivityChoiceRunes || strings.ContainsAny(choice, "\r\n") {
			return nil, ErrInvalidActivity
		}
		if _, exists := seen[choice]; exists {
			return nil, ErrInvalidActivity
		}
		seen[choice] = struct{}{}
		result = append(result, choice)
	}
	return result, nil
}

func normalizeActivityResponseChoices(activity Activity, choices []string) ([]string, error) {
	if activity.Kind == ActivityKindNotice {
		if len(choices) != 0 {
			return nil, ErrInvalidActivity
		}
		return []string{}, nil
	}
	if len(choices) > len(activity.Choices) {
		return nil, ErrInvalidActivity
	}
	if !activity.Multiple && len(choices) > 1 {
		return nil, ErrInvalidActivity
	}
	allowed := make(map[string]struct{}, len(activity.Choices))
	for _, choice := range activity.Choices {
		allowed[choice] = struct{}{}
	}
	result := make([]string, 0, len(choices))
	seen := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		choice = strings.TrimSpace(choice)
		if _, exists := allowed[choice]; !exists {
			return nil, ErrInvalidActivity
		}
		if _, exists := seen[choice]; exists {
			continue
		}
		seen[choice] = struct{}{}
		result = append(result, choice)
	}
	return result, nil
}

func validActivityID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

func encodeActivityCursor(cursor activityCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeActivityCursor(value string) (activityCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) > 256 {
		return activityCursor{}, ErrInvalidActivity
	}
	var cursor activityCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.CreatedAt <= 0 || !validActivityID(cursor.ID) {
		return activityCursor{}, ErrInvalidActivity
	}
	return cursor, nil
}
