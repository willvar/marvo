package store

import (
	"errors"
	"strings"
	"testing"
)

func TestBrandStoreDefaultsAndPersistsInUserState(t *testing.T) {
	state, _ := newTestStateDB(t)
	brandStore, err := NewBrandStore(state)
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
	reloaded, err := NewBrandStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().Name; got != "我的知识库" {
		t.Fatalf("reloaded brand = %q", got)
	}
}

func TestBrandStoreRejectsInvalidNames(t *testing.T) {
	state, _ := newTestStateDB(t)
	brandStore, err := NewBrandStore(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "\n", strings.Repeat("界", MaxBrandRunes+1)} {
		if _, err := brandStore.Save(name); !errors.Is(err, ErrInvalidBrand) {
			t.Fatalf("Save(%q) error = %v, want ErrInvalidBrand", name, err)
		}
	}
}
