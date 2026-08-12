package runtimeauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadOrCreateToken(path string) (string, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("runtime token path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("create runtime token directory: %w", err)
	}
	if token, err := readToken(path); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate runtime token: %w", err)
	}
	token := hex.EncodeToString(random)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".runtime-token-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return "", err
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	// Another process may have initialized the token between our read and write.
	// A hard link gives us create-if-absent semantics without replacing its key.
	if err := os.Link(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		if errors.Is(err, os.ErrExist) {
			return readToken(path)
		}
		return "", err
	}
	_ = os.Remove(tmpPath)
	return token, nil
}

func readToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("runtime token is not a regular file")
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", errors.New("runtime token permissions are too broad")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 32 {
		return "", errors.New("runtime token is invalid")
	}
	return token, nil
}
