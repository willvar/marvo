//go:build marvo_web

package webapp

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"
)

func TestEmbeddedDistributionContainsEveryBuiltFile(t *testing.T) {
	built := os.DirFS("dist")
	count := 0
	err := fs.WalkDir(built, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		count++
		embeddedName := path.Join("dist", filepath.ToSlash(name))
		if _, statErr := fs.Stat(assets, embeddedName); statErr != nil {
			t.Errorf("built file %q is missing from embedded distribution: %v", name, statErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("frontend/dist contains no files")
	}
}
