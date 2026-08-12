package runtimeauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenPersistsPrivateValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control", ".runtime-token")
	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) < 32 {
		t.Fatalf("tokens differ or are too short")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("token mode = %04o", info.Mode().Perm())
	}
}
