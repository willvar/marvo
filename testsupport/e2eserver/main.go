package main

import (
	"fmt"
	"os"
	"path/filepath"

	"marvo/cmd"
)

func main() {
	testData, err := filepath.Abs(filepath.Join("frontend", "e2e", ".data"))
	if err != nil {
		panic(err)
	}
	expectedParent, err := filepath.Abs(filepath.Join("frontend", "e2e"))
	if err != nil {
		panic(err)
	}
	if filepath.Base(testData) != ".data" || filepath.Dir(testData) != expectedParent {
		panic(fmt.Sprintf("refusing to reset unexpected E2E path %q", testData))
	}
	if err := os.RemoveAll(testData); err != nil {
		panic(fmt.Errorf("reset E2E data: %w", err))
	}
	if err := os.MkdirAll(testData, 0700); err != nil {
		panic(fmt.Errorf("create E2E data: %w", err))
	}
	legacyNote := filepath.Join(testData, "legacy", "Legacy E2E note")
	if err := os.MkdirAll(legacyNote, 0700); err != nil {
		panic(fmt.Errorf("create E2E legacy note: %w", err))
	}
	if err := os.WriteFile(filepath.Join(legacyNote, "index.md"), []byte("legacy migration content\n"), 0600); err != nil {
		panic(fmt.Errorf("write E2E legacy content: %w", err))
	}
	if err := os.WriteFile(filepath.Join(legacyNote, "meta.json"), []byte("{\"tags\":[\"legacy\"]}\n"), 0600); err != nil {
		panic(fmt.Errorf("write E2E legacy metadata: %w", err))
	}
	os.Args = []string{os.Args[0], "-c", "frontend/e2e/config.yaml"}
	cmd.Execute()
}
