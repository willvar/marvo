package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxConnectorNameRunes  = 100
	MaxConnectorConfigSize = 128 << 10
	MaxDeliveryErrorRunes  = 1000
)

var (
	ErrInvalidConnector  = errors.New("invalid connector")
	ErrConnectorNotFound = errors.New("connector not found")
	ErrDeliveryNotFound  = errors.New("delivery not found")
)

type Connector struct {
	ID         string         `json:"id"`
	ProviderID string         `json:"provider_id"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Config     map[string]any `json:"-"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type ConnectorDeliverySummary struct {
	Pending   int        `json:"pending"`
	Sent      int        `json:"sent"`
	Failed    int        `json:"failed"`
	LastError string     `json:"last_error,omitempty"`
	LastSent  *time.Time `json:"last_sent_at,omitempty"`
}

type ClaimedDelivery struct {
	ID        string
	Attempt   int
	Connector Connector
	Activity  Activity
	CreatedAt time.Time
}

type ConnectorStore struct {
	state  *StateDB
	userID string
	key    [32]byte
}

func NewConnectorStore(state *StateDB, userID, masterSecret string) (*ConnectorStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	if strings.TrimSpace(userID) == "" || len(userID) > 128 || len(masterSecret) < 32 {
		return nil, errors.New("connector encryption context is invalid")
	}
	mac := hmac.New(sha256.New, []byte(masterSecret))
	_, _ = mac.Write([]byte("marvo-connector-config-v1\x00" + userID))
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	return &ConnectorStore{state: state, userID: userID, key: key}, nil
}

func (s *ConnectorStore) List() ([]Connector, error) {
	rows, err := s.state.sql.Query(`
		SELECT id, provider_id, name, enabled, config_nonce, config_ciphertext, created_at, updated_at
		FROM connectors ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Connector, 0)
	for rows.Next() {
		connector, err := s.scanConnector(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, connector)
	}
	return result, rows.Err()
}

func (s *ConnectorStore) Get(id string) (Connector, error) {
	if !validConnectorID(id) {
		return Connector{}, ErrConnectorNotFound
	}
	return s.scanConnector(s.state.sql.QueryRow(`
		SELECT id, provider_id, name, enabled, config_nonce, config_ciphertext, created_at, updated_at
		FROM connectors WHERE id = ?
	`, id))
}

func (s *ConnectorStore) Create(providerID, name string, enabled bool, config map[string]any) (Connector, error) {
	providerID, name, payload, err := normalizeConnector(providerID, name, config)
	if err != nil {
		return Connector{}, err
	}
	id, err := randomID()
	if err != nil {
		return Connector{}, err
	}
	nonce, ciphertext, err := s.encrypt(id, providerID, payload)
	if err != nil {
		return Connector{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = s.state.sql.Exec(`
		INSERT INTO connectors(id, provider_id, name, enabled, config_nonce, config_ciphertext, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`, id, providerID, name, enabled, nonce, ciphertext, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Connector{}, fmt.Errorf("create connector: %w", err)
	}
	return Connector{ID: id, ProviderID: providerID, Name: name, Enabled: enabled, Config: cloneConnectorConfig(config), CreatedAt: now, UpdatedAt: now}, nil
}

func (s *ConnectorStore) Update(id, name string, enabled bool, config map[string]any) (Connector, error) {
	existing, err := s.Get(id)
	if err != nil {
		return Connector{}, err
	}
	_, name, payload, err := normalizeConnector(existing.ProviderID, name, config)
	if err != nil {
		return Connector{}, err
	}
	nonce, ciphertext, err := s.encrypt(existing.ID, existing.ProviderID, payload)
	if err != nil {
		return Connector{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return Connector{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`
		UPDATE connectors
		SET name = ?, enabled = ?, config_nonce = ?, config_ciphertext = ?, updated_at = ?
		WHERE id = ?
	`, name, enabled, nonce, ciphertext, now.UnixMilli(), existing.ID)
	if err != nil {
		return Connector{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Connector{}, ErrConnectorNotFound
	}
	if !enabled {
		if _, err := tx.Exec(`
			UPDATE activity_deliveries
			SET status = 'cancelled', lease_until = NULL, updated_at = ?
			WHERE connector_id = ? AND status IN ('pending', 'sending')
		`, now.UnixMilli(), existing.ID); err != nil {
			return Connector{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Connector{}, err
	}
	existing.Name = name
	existing.Enabled = enabled
	existing.Config = cloneConnectorConfig(config)
	existing.UpdatedAt = now
	return existing, nil
}

func (s *ConnectorStore) Delete(id string) (bool, error) {
	if !validConnectorID(id) {
		return false, ErrInvalidConnector
	}
	result, err := s.state.sql.Exec(`DELETE FROM connectors WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *ConnectorStore) Summary(connectorID string) (ConnectorDeliverySummary, error) {
	if !validConnectorID(connectorID) {
		return ConnectorDeliverySummary{}, ErrInvalidConnector
	}
	var summary ConnectorDeliverySummary
	var lastSent sql.NullInt64
	if err := s.state.sql.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN status IN ('pending', 'sending') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE((SELECT last_error FROM activity_deliveries
				WHERE connector_id = ? AND last_error != '' ORDER BY updated_at DESC, id DESC LIMIT 1), ''),
			MAX(sent_at)
		FROM activity_deliveries WHERE connector_id = ?
	`, connectorID, connectorID).Scan(&summary.Pending, &summary.Sent, &summary.Failed, &summary.LastError, &lastSent); err != nil {
		return ConnectorDeliverySummary{}, err
	}
	if lastSent.Valid {
		value := time.UnixMilli(lastSent.Int64).UTC()
		summary.LastSent = &value
	}
	return summary, nil
}

func (s *ConnectorStore) RetryConnectorFailures(connectorID string, now time.Time) (int64, error) {
	if !validConnectorID(connectorID) {
		return 0, ErrInvalidConnector
	}
	result, err := s.state.sql.Exec(`
		UPDATE activity_deliveries
		SET status = 'pending', attempt_count = 0, next_attempt_at = ?, lease_until = NULL,
			last_error = '', sent_at = NULL, updated_at = ?
		WHERE connector_id = ? AND status = 'failed'
	`, now.UTC().UnixMilli(), now.UTC().UnixMilli(), connectorID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *ConnectorStore) ClaimDue(now time.Time, lease time.Duration) (*ClaimedDelivery, error) {
	now = now.UTC()
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var deliveryID string
	err = tx.QueryRow(`
		SELECT d.id
		FROM activity_deliveries d
		JOIN connectors c ON c.id = d.connector_id
		WHERE c.enabled = 1 AND (
			(d.status = 'pending' AND d.next_attempt_at <= ?) OR
			(d.status = 'sending' AND d.lease_until IS NOT NULL AND d.lease_until <= ?)
		)
		ORDER BY CASE WHEN d.status = 'sending' THEN d.lease_until ELSE d.next_attempt_at END, d.created_at, d.id
		LIMIT 1
	`, now.UnixMilli(), now.UnixMilli()).Scan(&deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(lease).UnixMilli()
	result, err := tx.Exec(`
		UPDATE activity_deliveries
		SET status = 'sending', attempt_count = attempt_count + 1, lease_until = ?, updated_at = ?
		WHERE id = ? AND (
			(status = 'pending' AND next_attempt_at <= ?) OR
			(status = 'sending' AND lease_until IS NOT NULL AND lease_until <= ?)
		)
	`, leaseUntil, now.UnixMilli(), deliveryID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, nil
	}
	claimed, err := s.scanClaimedDelivery(tx.QueryRow(deliverySelect+` WHERE d.id = ?`, deliveryID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &claimed, nil
}

func (s *ConnectorStore) NextDue() (*time.Time, error) {
	var value sql.NullInt64
	if err := s.state.sql.QueryRow(`
		SELECT MIN(CASE WHEN d.status = 'sending' THEN d.lease_until ELSE d.next_attempt_at END)
		FROM activity_deliveries d
		JOIN connectors c ON c.id = d.connector_id
		WHERE c.enabled = 1 AND d.status IN ('pending', 'sending')
	`).Scan(&value); err != nil {
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}
	next := time.UnixMilli(value.Int64).UTC()
	return &next, nil
}

func (s *ConnectorStore) MarkSent(id string, now time.Time) error {
	result, err := s.state.sql.Exec(`
		UPDATE activity_deliveries
		SET status = 'sent', lease_until = NULL, last_error = '', sent_at = ?, updated_at = ?
		WHERE id = ? AND status = 'sending'
	`, now.UTC().UnixMilli(), now.UTC().UnixMilli(), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (s *ConnectorStore) MarkFailed(id string, now, next time.Time, final bool, message string) error {
	message = truncateRunes(strings.TrimSpace(message), MaxDeliveryErrorRunes)
	status := "pending"
	if final {
		status = "failed"
	}
	result, err := s.state.sql.Exec(`
		UPDATE activity_deliveries
		SET status = ?, next_attempt_at = ?, lease_until = NULL, last_error = ?, updated_at = ?
		WHERE id = ? AND status = 'sending'
	`, status, next.UTC().UnixMilli(), message, now.UTC().UnixMilli(), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (s *ConnectorStore) RetryFailed(id string, now time.Time) (bool, error) {
	if !validConnectorID(id) {
		return false, ErrInvalidConnector
	}
	result, err := s.state.sql.Exec(`
		UPDATE activity_deliveries
		SET status = 'pending', attempt_count = 0, next_attempt_at = ?, lease_until = NULL,
			last_error = '', sent_at = NULL, updated_at = ?
		WHERE id = ? AND status = 'failed'
	`, now.UTC().UnixMilli(), now.UTC().UnixMilli(), id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *ConnectorStore) scanConnector(row rowScanner) (Connector, error) {
	var connector Connector
	var nonce, ciphertext string
	var createdAt, updatedAt int64
	if err := row.Scan(&connector.ID, &connector.ProviderID, &connector.Name, &connector.Enabled, &nonce, &ciphertext, &createdAt, &updatedAt); errors.Is(err, sql.ErrNoRows) {
		return Connector{}, ErrConnectorNotFound
	} else if err != nil {
		return Connector{}, err
	}
	payload, err := s.decrypt(connector.ID, connector.ProviderID, nonce, ciphertext)
	if err != nil {
		return Connector{}, fmt.Errorf("decrypt connector %s: %w", connector.ID, err)
	}
	if err := json.Unmarshal(payload, &connector.Config); err != nil {
		return Connector{}, fmt.Errorf("decode connector %s: %w", connector.ID, err)
	}
	connector.CreatedAt = time.UnixMilli(createdAt).UTC()
	connector.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return connector, nil
}

func (s *ConnectorStore) scanClaimedDelivery(row rowScanner) (ClaimedDelivery, error) {
	var result ClaimedDelivery
	var nonce, ciphertext, choicesJSON string
	var createdAt, activityCreatedAt, connectorCreatedAt, connectorUpdatedAt int64
	if err := row.Scan(
		&result.ID, &result.Attempt, &createdAt,
		&result.Connector.ID, &result.Connector.ProviderID, &result.Connector.Name, &result.Connector.Enabled,
		&nonce, &ciphertext, &connectorCreatedAt, &connectorUpdatedAt,
		&result.Activity.ID, &result.Activity.Kind, &result.Activity.Title, &result.Activity.Content,
		&choicesJSON, &result.Activity.Multiple, &result.Activity.SourceSessionID, &result.Activity.SourceMessageID,
		&activityCreatedAt,
	); err != nil {
		return ClaimedDelivery{}, err
	}
	payload, err := s.decrypt(result.Connector.ID, result.Connector.ProviderID, nonce, ciphertext)
	if err != nil {
		return ClaimedDelivery{}, err
	}
	if err := json.Unmarshal(payload, &result.Connector.Config); err != nil {
		return ClaimedDelivery{}, err
	}
	if err := json.Unmarshal([]byte(choicesJSON), &result.Activity.Choices); err != nil {
		return ClaimedDelivery{}, err
	}
	result.CreatedAt = time.UnixMilli(createdAt).UTC()
	result.Activity.CreatedAt = time.UnixMilli(activityCreatedAt).UTC()
	result.Connector.CreatedAt = time.UnixMilli(connectorCreatedAt).UTC()
	result.Connector.UpdatedAt = time.UnixMilli(connectorUpdatedAt).UTC()
	return result, nil
}

func (s *ConnectorStore) encrypt(id, providerID string, payload []byte) (string, string, error) {
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	sealed := gcm.Seal(nil, nonce, payload, s.additionalData(id, providerID))
	return base64.RawURLEncoding.EncodeToString(nonce), base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *ConnectorStore) decrypt(id, providerID, encodedNonce, encodedCiphertext string) ([]byte, error) {
	nonce, err := base64.RawURLEncoding.DecodeString(encodedNonce)
	if err != nil {
		return nil, ErrInvalidConnector
	}
	sealed, err := base64.RawURLEncoding.DecodeString(encodedCiphertext)
	if err != nil || len(sealed) > MaxConnectorConfigSize+64 {
		return nil, ErrInvalidConnector
	}
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() || len(sealed) < gcm.Overhead() {
		return nil, ErrInvalidConnector
	}
	plain, err := gcm.Open(nil, nonce, sealed, s.additionalData(id, providerID))
	if err != nil {
		return nil, ErrInvalidConnector
	}
	return plain, nil
}

func (s *ConnectorStore) additionalData(id, providerID string) []byte {
	return []byte("marvo-connector-config-v1\x00" + s.userID + "\x00" + id + "\x00" + providerID)
}

func normalizeConnector(providerID, name string, config map[string]any) (string, string, []byte, error) {
	providerID = strings.TrimSpace(providerID)
	name = strings.TrimSpace(name)
	if providerID == "" || len(providerID) > 64 || !utf8.ValidString(providerID) ||
		name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxConnectorNameRunes {
		return "", "", nil, ErrInvalidConnector
	}
	for _, character := range providerID {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return "", "", nil, ErrInvalidConnector
		}
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", "", nil, ErrInvalidConnector
		}
	}
	if config == nil {
		config = map[string]any{}
	}
	payload, err := json.Marshal(config)
	if err != nil || len(payload) > MaxConnectorConfigSize {
		return "", "", nil, ErrInvalidConnector
	}
	return providerID, name, payload, nil
}

func cloneConnectorConfig(config map[string]any) map[string]any {
	payload, _ := json.Marshal(config)
	var cloned map[string]any
	_ = json.Unmarshal(payload, &cloned)
	if cloned == nil {
		cloned = map[string]any{}
	}
	return cloned
}

func validConnectorID(id string) bool {
	return validActivityID(id)
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

const deliverySelect = `
	SELECT d.id, d.attempt_count, d.created_at,
		c.id, c.provider_id, c.name, c.enabled, c.config_nonce, c.config_ciphertext, c.created_at, c.updated_at,
		a.id, a.kind, a.title, a.content, a.choices_json, a.multiple,
		a.source_session_id, a.source_message_id, a.created_at
	FROM activity_deliveries d
	JOIN connectors c ON c.id = d.connector_id
	JOIN activities a ON a.id = d.activity_id`
