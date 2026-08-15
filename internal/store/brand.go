package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	mu     sync.RWMutex
	path   string
	config BrandConfig
}

func NewBrandStore(appDir string) (*BrandStore, error) {
	brandStore := &BrandStore{
		path:   filepath.Join(appDir, brandFilename),
		config: BrandConfig{Name: DefaultBrandName},
	}
	if err := brandStore.load(); err != nil {
		return nil, err
	}
	return brandStore, nil
}

func (s *BrandStore) Get() BrandConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *BrandStore) Save(name string) (BrandConfig, error) {
	config := BrandConfig{Name: strings.TrimSpace(name)}
	if err := ValidateBrandName(config.Name); err != nil {
		return BrandConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateBrandFile(s.path); err != nil {
		return BrandConfig{}, err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return BrandConfig{}, err
	}
	data = append(data, '\n')
	if err := writePrivateFileAtomic(s.path, data); err != nil {
		return BrandConfig{}, err
	}
	s.config = config
	return config, nil
}

func (s *BrandStore) load() error {
	if err := validateBrandFile(s.path); err != nil {
		return err
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() > maxBrandFileBytes {
		return fmt.Errorf("%w: file is too large", ErrInvalidBrand)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var config BrandConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBrand, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: file must contain one JSON value", ErrInvalidBrand)
	}
	config.Name = strings.TrimSpace(config.Name)
	if err := ValidateBrandName(config.Name); err != nil {
		return err
	}
	s.config = config
	return nil
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

func validateBrandFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: path is not a regular file", ErrInvalidBrand)
	}
	return nil
}
