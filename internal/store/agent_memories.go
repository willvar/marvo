package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	legacyMemoriesFilename = ".agent-personalization.json"
	MaxMemoriesBytes       = 256 << 10
	MaxMemoryBytes         = 4 << 10
	MaxMemories            = 256
)

var (
	ErrInvalidMemories = errors.New("invalid memories")
	ErrMemoriesChanged = errors.New("memories changed")
	memoryIDPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type Memory struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type MemorySnapshot struct {
	Memories []Memory `json:"memories"`
	Revision string   `json:"revision"`
}

type legacyMemoriesFile struct {
	Rules []Memory `json:"rules"`
}

type MemoryStore struct {
	state *StateDB
}

func NewMemoryStore(state *StateDB) (*MemoryStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	store := &MemoryStore{state: state}
	if _, err := store.Get(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MemoryStore) Get() (MemorySnapshot, error) {
	if s == nil || s.state == nil {
		return MemorySnapshot{}, errors.New("memory store is unavailable")
	}
	return loadMemories(context.Background(), s.state.sql)
}

func (s *MemoryStore) Save(expectedRevision string, memories []Memory) (MemorySnapshot, error) {
	if s == nil || s.state == nil {
		return MemorySnapshot{}, errors.New("memory store is unavailable")
	}
	normalized, err := normalizeMemories(memories, true)
	if err != nil {
		return MemorySnapshot{}, err
	}
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return MemorySnapshot{}, err
	}
	defer tx.Rollback()
	current, err := loadMemories(context.Background(), tx)
	if err != nil {
		return MemorySnapshot{}, err
	}
	if expectedRevision == "" || expectedRevision != current.Revision {
		return current, ErrMemoriesChanged
	}
	if _, err := tx.Exec(`DELETE FROM memories`); err != nil {
		return MemorySnapshot{}, err
	}
	now := time.Now().UTC().UnixMilli()
	for position, memory := range normalized {
		if _, err := tx.Exec(`
			INSERT INTO memories(id, text, position, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?)
		`, memory.ID, memory.Text, position, now, now); err != nil {
			return MemorySnapshot{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return MemorySnapshot{}, err
	}
	return memorySnapshot(normalized)
}

func (s *MemoryStore) Add(text string) (Memory, error) {
	text, err := normalizeMemoryText(text)
	if err != nil {
		return Memory{}, err
	}
	tx, err := s.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback()
	var count int
	var position int
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(MAX(position), -1) + 1 FROM memories`).Scan(&count, &position); err != nil {
		return Memory{}, err
	}
	if count >= MaxMemories {
		return Memory{}, fmt.Errorf("%w: too many memories", ErrInvalidMemories)
	}
	var existing Memory
	err = tx.QueryRow(`SELECT id, text FROM memories WHERE text = ?`, text).Scan(&existing.ID, &existing.Text)
	if err == nil {
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Memory{}, err
	}
	id, err := newMemoryID()
	if err != nil {
		return Memory{}, err
	}
	now := time.Now().UTC().UnixMilli()
	memory := Memory{ID: id, Text: text}
	if _, err := tx.Exec(`
		INSERT INTO memories(id, text, position, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?)
	`, memory.ID, memory.Text, position, now, now); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, err
	}
	return memory, nil
}

func (s *MemoryStore) Update(id, text string) (Memory, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !memoryIDPattern.MatchString(id) {
		return Memory{}, fmt.Errorf("%w: invalid memory ID", ErrInvalidMemories)
	}
	text, err := normalizeMemoryText(text)
	if err != nil {
		return Memory{}, err
	}
	result, err := s.state.sql.Exec(`UPDATE memories SET text = ?, updated_at = ? WHERE id = ?`, text, time.Now().UTC().UnixMilli(), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Memory{}, fmt.Errorf("%w: duplicate memory text", ErrInvalidMemories)
		}
		return Memory{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Memory{}, sql.ErrNoRows
	}
	return Memory{ID: id, Text: text}, nil
}

func (s *MemoryStore) Remove(id string) (bool, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !memoryIDPattern.MatchString(id) {
		return false, fmt.Errorf("%w: invalid memory ID", ErrInvalidMemories)
	}
	result, err := s.state.sql.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	return true, nil
}

type memoryRows interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadMemories(ctx context.Context, source memoryRows) (MemorySnapshot, error) {
	rows, err := source.QueryContext(ctx, `SELECT id, text FROM memories ORDER BY position, id`)
	if err != nil {
		return MemorySnapshot{}, err
	}
	defer rows.Close()
	memories := make([]Memory, 0)
	for rows.Next() {
		var memory Memory
		if err := rows.Scan(&memory.ID, &memory.Text); err != nil {
			return MemorySnapshot{}, err
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return MemorySnapshot{}, err
	}
	normalized, err := normalizeMemories(memories, false)
	if err != nil {
		return MemorySnapshot{}, err
	}
	return memorySnapshot(normalized)
}

func memorySnapshot(memories []Memory) (MemorySnapshot, error) {
	data, err := json.Marshal(struct {
		Memories []Memory `json:"memories"`
	}{Memories: memories})
	if err != nil {
		return MemorySnapshot{}, err
	}
	digest := sha256.Sum256(data)
	return MemorySnapshot{Memories: copyMemories(memories), Revision: hex.EncodeToString(digest[:])}, nil
}

func normalizeMemories(memories []Memory, assignIDs bool) ([]Memory, error) {
	if len(memories) > MaxMemories {
		return nil, fmt.Errorf("%w: too many memories", ErrInvalidMemories)
	}
	result := make([]Memory, 0, len(memories))
	ids := make(map[string]struct{}, len(memories))
	texts := make(map[string]struct{}, len(memories))
	for _, memory := range memories {
		memory.ID = strings.ToLower(strings.TrimSpace(memory.ID))
		var err error
		memory.Text, err = normalizeMemoryText(memory.Text)
		if err != nil {
			return nil, err
		}
		if memory.ID == "" && assignIDs {
			memory.ID, err = newMemoryID()
			if err != nil {
				return nil, err
			}
		}
		if !memoryIDPattern.MatchString(memory.ID) {
			return nil, fmt.Errorf("%w: invalid memory ID", ErrInvalidMemories)
		}
		if _, exists := ids[memory.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate memory ID", ErrInvalidMemories)
		}
		if _, exists := texts[memory.Text]; exists {
			return nil, fmt.Errorf("%w: duplicate memory text", ErrInvalidMemories)
		}
		ids[memory.ID] = struct{}{}
		texts[memory.Text] = struct{}{}
		result = append(result, memory)
	}
	return result, nil
}

func normalizeMemoryText(value string) (string, error) {
	text := strings.TrimSpace(value)
	if !utf8.ValidString(text) || text == "" || len(text) > MaxMemoryBytes || strings.ContainsAny(text, "\r\n") {
		return "", fmt.Errorf("%w: invalid memory text", ErrInvalidMemories)
	}
	return text, nil
}

func newMemoryID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func copyMemories(memories []Memory) []Memory {
	result := make([]Memory, len(memories))
	copy(result, memories)
	return result
}
