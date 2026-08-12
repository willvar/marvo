package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrandStoreDefaultsAndPersistsPrivateConfiguration(t *testing.T) {
	appDir := t.TempDir()
	brandStore, err := NewBrandStore(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := brandStore.Get().Name; got != DefaultBrandName {
		t.Fatalf("default brand = %q, want %q", got, DefaultBrandName)
	}
	brand, err := brandStore.Save("  我的知识库  ")
	if err != nil {
		t.Fatal(err)
	}
	if brand.Name != "我的知识库" {
		t.Fatalf("saved brand = %#v", brand)
	}
	path := filepath.Join(appDir, brandFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("brand mode = %o, want 600", info.Mode().Perm())
	}
	reloaded, err := NewBrandStore(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().Name; got != "我的知识库" {
		t.Fatalf("reloaded brand = %q", got)
	}
}

func TestBrandStoreRejectsInvalidNamesAndSymlinks(t *testing.T) {
	appDir := t.TempDir()
	brandStore, err := NewBrandStore(appDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "\n", strings.Repeat("界", MaxBrandRunes+1)} {
		if _, err := brandStore.Save(name); !errors.Is(err, ErrInvalidBrand) {
			t.Fatalf("Save(%q) error = %v, want ErrInvalidBrand", name, err)
		}
	}
	target := filepath.Join(appDir, "target.json")
	if err := os.WriteFile(target, []byte(`{"name":"untouched"}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(appDir, brandFilename)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBrandStore(appDir); !errors.Is(err, ErrInvalidBrand) {
		t.Fatalf("symlink load error = %v, want ErrInvalidBrand", err)
	}
}
