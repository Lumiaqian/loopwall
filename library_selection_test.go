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

func TestLibraryShortcutBeforeAccess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Documents")
	entry, err := createLibraryShortcut(root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(entry))
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("target changed: %v", err)
	}
	if got, err := os.Readlink(entry); err != nil || got != root {
		t.Fatalf("shortcut: %q %v", got, err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := selectedLibraryDirectory(root, []string{entry}); err != nil || got != entry {
		t.Fatalf("selection: %q %v", got, err)
	}
	if _, err := selectedLibraryDirectory(root, []string{filepath.Dir(entry)}); err == nil {
		t.Fatal("accepted staging directory")
	}
}
