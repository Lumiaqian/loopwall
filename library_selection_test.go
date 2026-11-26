package main

import (
	"github.com/Lumiaqian/loopwall/internal/catalog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedLibraryDirectory(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, alias} {
		selected, err := selectedLibraryDirectory(root, []string{path})
		if err != nil || selected != path {
			t.Fatalf("selection %q: got %q, %v", path, selected, err)
		}
		if _, err := catalog.LoadExisting(selected); !os.IsNotExist(err) {
			t.Fatalf("missing catalog: %v", err)
		}
	}
	other := t.TempDir()
	for _, paths := range [][]string{{other}, {root, other}} {
		if _, err := selectedLibraryDirectory(root, paths); err == nil || !strings.Contains(err.Error(), other) {
			t.Fatalf("unexpected selection %q: %v", paths, err)
		}
	}
}
