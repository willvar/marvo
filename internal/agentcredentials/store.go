package agentcredentials

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

	"marvo/internal/userid"
)

const (
	credentialsFilename = ".agent-credentials.json"
	credentialsVersion  = 1
	maxCredentialsBytes = 16 << 10
	MaxExaAPIKeyBytes   = 4 << 10
)

var ErrInvalidCredentials = errors.New("invalid Agent credentials")

type Credentials struct {
	ExaAPIKey string `json:"exa_api_key"`
}

type encryptedCredentials struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type Store struct {
	mu             sync.Mutex
	path           string
	userID         string
	encryptionKey  [32]byte
	fingerprintKey [32]byte
}

func NewStore(dataDir, userID, masterSecret string) (*Store, error) {
	dataDir = filepath.Clean(dataDir)
	if !filepath.IsAbs(dataDir) {
		return nil, errors.New("agent credential directory must be absolute")
	}
	if !userid.Valid(userID) {
		return nil, errors.New("invalid user id")
	}
	if len(masterSecret) < 32 {
		return nil, errors.New("agent credentials require a master secret of at least 32 characters")
	}
	if err := validatePrivateDirectory(dataDir); err != nil {
		return nil, err
	}
	return &Store{
		path:           filepath.Join(dataDir, credentialsFilename),
		userID:         userID,
		encryptionKey:  deriveKey(masterSecret, "encryption", userID),
		fingerprintKey: deriveKey(masterSecret, "fingerprint", userID),
	}, nil
}

func (s *Store) Load() (Credentials, error) {
	if s == nil {
		return Credentials{}, errors.New("agent credential store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) Save(credentials Credentials) error {
	if s == nil {
		return errors.New("agent credential store is unavailable")
	}
	normalized, err := Normalize(credentials)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validatePrivateDirectory(filepath.Dir(s.path)); err != nil {
		return err
	}
	if err := validateCredentialFileOrMissing(s.path); err != nil {
		return err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.encryptionKey[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nil, nonce, payload, s.additionalData())
	envelope := encryptedCredentials{
		Version:    credentialsVersion,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(sealed),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writePrivateFileAtomic(s.path, data)
}

func (s *Store) Fingerprint(credentials Credentials) string {
	if s == nil || credentials.ExaAPIKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.fingerprintKey[:])
	_, _ = mac.Write([]byte("marvo-agent-credentials-fingerprint-v1\x00"))
	_, _ = mac.Write([]byte(credentials.ExaAPIKey))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

func Normalize(credentials Credentials) (Credentials, error) {
	credentials.ExaAPIKey = strings.TrimSpace(credentials.ExaAPIKey)
	if !utf8.ValidString(credentials.ExaAPIKey) || len(credentials.ExaAPIKey) > MaxExaAPIKeyBytes {
		return Credentials{}, fmt.Errorf("%w: Exa API key is invalid", ErrInvalidCredentials)
	}
	for _, character := range credentials.ExaAPIKey {
		if unicode.IsControl(character) {
			return Credentials{}, fmt.Errorf("%w: Exa API key contains control characters", ErrInvalidCredentials)
		}
	}
	return credentials, nil
}

func (s *Store) load() (Credentials, error) {
	if err := validatePrivateDirectory(filepath.Dir(s.path)); err != nil {
		return Credentials{}, err
	}
	if err := validateCredentialFileOrMissing(s.path); err != nil {
		return Credentials{}, err
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, err
	}
	if info.Size() > maxCredentialsBytes {
		return Credentials{}, fmt.Errorf("%w: credential file is too large", ErrInvalidCredentials)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return Credentials{}, err
	}
	var envelope encryptedCredentials
	if err := decodeSingleJSON(data, &envelope); err != nil {
		return Credentials{}, fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
	}
	if envelope.Version != credentialsVersion || envelope.Nonce == "" || envelope.Ciphertext == "" {
		return Credentials{}, fmt.Errorf("%w: unsupported credential format", ErrInvalidCredentials)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return Credentials{}, fmt.Errorf("%w: invalid credential nonce", ErrInvalidCredentials)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return Credentials{}, fmt.Errorf("%w: invalid credential payload", ErrInvalidCredentials)
	}
	block, err := aes.NewCipher(s.encryptionKey[:])
	if err != nil {
		return Credentials{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Credentials{}, err
	}
	if len(nonce) != gcm.NonceSize() || len(sealed) < gcm.Overhead() {
		return Credentials{}, fmt.Errorf("%w: invalid credential payload", ErrInvalidCredentials)
	}
	plain, err := gcm.Open(nil, nonce, sealed, s.additionalData())
	if err != nil {
		return Credentials{}, fmt.Errorf("%w: cannot decrypt credential payload", ErrInvalidCredentials)
	}
	var credentials Credentials
	if err := decodeSingleJSON(plain, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
	}
	return Normalize(credentials)
}

func (s *Store) additionalData() []byte {
	return []byte("marvo-agent-credentials-v1\x00" + s.userID)
}

func deriveKey(masterSecret, purpose, userID string) [32]byte {
	mac := hmac.New(sha256.New, []byte(masterSecret))
	_, _ = mac.Write([]byte("marvo-agent-credentials-v1\x00" + purpose + "\x00" + userID))
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	return key
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("credential file must contain one JSON value")
		}
		return err
	}
	return nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Agent credential directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("agent credential directory is not a regular directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("agent credential directory permissions are too broad")
	}
	return nil
}

func validateCredentialFileOrMissing(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: credential path is not a regular file", ErrInvalidCredentials)
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%w: credential file permissions are too broad", ErrInvalidCredentials)
	}
	return nil
}

func writePrivateFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".agent-credentials-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if directoryFile, err := os.Open(directory); err == nil {
		_ = directoryFile.Sync()
		_ = directoryFile.Close()
	}
	return nil
}
