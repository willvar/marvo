package handler

import (
	"marvo/internal/store"
	"testing"
)

func newHandlerStateDB(t *testing.T) *store.StateDB {
	t.Helper()
	state, err := store.OpenStateDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	return state
}
