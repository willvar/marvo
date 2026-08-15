package store

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultBrandName  = "Marvo"
	brandFilename     = ".brand.json"
	maxBrandFileBytes = 8 << 10
	MaxBrandRunes     = 100
)

var ErrInvalidBrand = errors.New("invalid brand configuration")

type BrandConfig struct {
	Name string `json:"name"`
}

type BrandStore struct {
	state *StateDB
}

func NewBrandStore(state *StateDB) (*BrandStore, error) {
	if state == nil || state.sql == nil {
		return nil, errors.New("user state database is unavailable")
	}
	brandStore := &BrandStore{state: state}
	if _, err := brandStore.load(); err != nil {
		return nil, err
	}
	return brandStore, nil
}

func (s *BrandStore) Get() BrandConfig {
	config, err := s.load()
	if err != nil {
		slog.Error("failed to read brand settings", "error", err)
		return BrandConfig{Name: DefaultBrandName}
	}
	return config
}

func (s *BrandStore) Save(name string) (BrandConfig, error) {
	config := BrandConfig{Name: strings.TrimSpace(name)}
	if err := ValidateBrandName(config.Name); err != nil {
		return BrandConfig{}, err
	}
	if _, err := s.state.sql.Exec(`
		UPDATE space_settings SET brand_name = ?, updated_at = ? WHERE id = 1
	`, config.Name, time.Now().UTC().UnixMilli()); err != nil {
		return BrandConfig{}, fmt.Errorf("save brand settings: %w", err)
	}
	return config, nil
}

func (s *BrandStore) load() (BrandConfig, error) {
	if s == nil || s.state == nil {
		return BrandConfig{}, errors.New("brand store is unavailable")
	}
	var config BrandConfig
	if err := s.state.sql.QueryRow(`SELECT brand_name FROM space_settings WHERE id = 1`).Scan(&config.Name); err != nil {
		return BrandConfig{}, fmt.Errorf("load brand settings: %w", err)
	}
	config.Name = strings.TrimSpace(config.Name)
	if err := ValidateBrandName(config.Name); err != nil {
		return BrandConfig{}, err
	}
	return config, nil
}

func ValidateBrandName(name string) error {
	if !utf8.ValidString(name) || name == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("%w: name cannot be empty or surrounded by whitespace", ErrInvalidBrand)
	}
	if utf8.RuneCountInString(name) > MaxBrandRunes {
		return fmt.Errorf("%w: name is too long", ErrInvalidBrand)
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return fmt.Errorf("%w: name contains control characters", ErrInvalidBrand)
		}
	}
	return nil
}
