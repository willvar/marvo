package store

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	agentPersonalizationFilename = ".agent-personalization.json"
	MaxPersonalizationBytes      = 256 << 10
	MaxPersonalizationRuleBytes  = 4 << 10
	MaxPersonalizationRules      = 256
)

var (
	ErrInvalidPersonalization = errors.New("invalid agent personalization")
	ErrPersonalizationChanged = errors.New("agent personalization changed")
	personalizationRuleID     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type PersonalizationRule struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type PersonalizationSnapshot struct {
	Rules    []PersonalizationRule `json:"rules"`
	Revision string                `json:"revision"`
}

type personalizationFile struct {
	Rules []PersonalizationRule `json:"rules"`
}

type AgentPersonalizationStore struct {
	mu   sync.Mutex
	path string
}

func NewAgentPersonalizationStore(dataDir string) (*AgentPersonalizationStore, error) {
	store := &AgentPersonalizationStore{path: filepath.Join(dataDir, agentPersonalizationFilename)}
	if _, err := store.Get(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *AgentPersonalizationStore) Get() (PersonalizationSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *AgentPersonalizationStore) Save(expectedRevision string, rules []PersonalizationRule) (PersonalizationSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.loadLocked()
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	if expectedRevision == "" || expectedRevision != current.Revision {
		return current, ErrPersonalizationChanged
	}
	normalized, err := normalizePersonalizationRules(rules, true)
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	data, revision, err := encodePersonalization(normalized)
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	if err := validateRegularFileOrMissing(s.path); err != nil {
		return PersonalizationSnapshot{}, err
	}
	if len(normalized) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PersonalizationSnapshot{}, err
		}
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return PersonalizationSnapshot{}, err
		}
	} else if err := writePrivateFileAtomic(s.path, data); err != nil {
		return PersonalizationSnapshot{}, err
	}
	return PersonalizationSnapshot{Rules: copyPersonalizationRules(normalized), Revision: revision}, nil
}

func (s *AgentPersonalizationStore) loadLocked() (PersonalizationSnapshot, error) {
	if err := validateRegularFileOrMissing(s.path); err != nil {
		return PersonalizationSnapshot{}, err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		_, revision, encodeErr := encodePersonalization(nil)
		return PersonalizationSnapshot{Rules: []PersonalizationRule{}, Revision: revision}, encodeErr
	}
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	if len(data) > MaxPersonalizationBytes {
		return PersonalizationSnapshot{}, fmt.Errorf("%w: file is too large", ErrInvalidPersonalization)
	}
	var file personalizationFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return PersonalizationSnapshot{}, fmt.Errorf("%w: %v", ErrInvalidPersonalization, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return PersonalizationSnapshot{}, fmt.Errorf("%w: file must contain one JSON value", ErrInvalidPersonalization)
	}
	rules, err := normalizePersonalizationRules(file.Rules, false)
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	_, revision, err := encodePersonalization(rules)
	if err != nil {
		return PersonalizationSnapshot{}, err
	}
	return PersonalizationSnapshot{Rules: copyPersonalizationRules(rules), Revision: revision}, nil
}

func normalizePersonalizationRules(rules []PersonalizationRule, assignIDs bool) ([]PersonalizationRule, error) {
	if len(rules) > MaxPersonalizationRules {
		return nil, fmt.Errorf("%w: too many rules", ErrInvalidPersonalization)
	}
	result := make([]PersonalizationRule, 0, len(rules))
	ids := make(map[string]struct{}, len(rules))
	texts := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		rule.ID = strings.ToLower(strings.TrimSpace(rule.ID))
		rule.Text = strings.TrimSpace(rule.Text)
		if rule.ID == "" && assignIDs {
			id, err := newPersonalizationRuleID()
			if err != nil {
				return nil, err
			}
			rule.ID = id
		}
		if !personalizationRuleID.MatchString(rule.ID) {
			return nil, fmt.Errorf("%w: invalid rule ID", ErrInvalidPersonalization)
		}
		if !utf8.ValidString(rule.Text) || rule.Text == "" || len(rule.Text) > MaxPersonalizationRuleBytes ||
			strings.ContainsAny(rule.Text, "\r\n") {
			return nil, fmt.Errorf("%w: invalid rule text", ErrInvalidPersonalization)
		}
		if _, exists := ids[rule.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate rule ID", ErrInvalidPersonalization)
		}
		if _, exists := texts[rule.Text]; exists {
			return nil, fmt.Errorf("%w: duplicate rule text", ErrInvalidPersonalization)
		}
		ids[rule.ID] = struct{}{}
		texts[rule.Text] = struct{}{}
		result = append(result, rule)
	}
	return result, nil
}

func encodePersonalization(rules []PersonalizationRule) ([]byte, string, error) {
	if rules == nil {
		rules = []PersonalizationRule{}
	}
	data, err := json.MarshalIndent(personalizationFile{Rules: rules}, "", "  ")
	if err != nil {
		return nil, "", err
	}
	data = append(data, '\n')
	digest := sha256.Sum256(data)
	return data, hex.EncodeToString(digest[:]), nil
}

func newPersonalizationRuleID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func copyPersonalizationRules(rules []PersonalizationRule) []PersonalizationRule {
	result := make([]PersonalizationRule, len(rules))
	copy(result, rules)
	return result
}
