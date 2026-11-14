package catalog

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// LegacyExtensionID is the extension's identifier when the app was
// Phosphene Go, whose container held the library before Loopwall.
const LegacyExtensionID = "local.lumiaqian.phosphenego.extension"

// LegacyRoot is the library's directory as Phosphene Go kept it.
func LegacyRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Containers", LegacyExtensionID, "Data", "Documents")
}

// Migrate moves the library at from to root once: when root has no
// catalog and from has a valid one. It moves each video's directory, then
// publishes the catalog, so a migration cut short is resumed the next
// time, and leaves from's catalog renamed, so it is not moved twice. It
// reports whether it moved a library.
func Migrate(from, root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, "catalog.json")); err == nil {
		return false, nil // root has its own library
	}
	c, err := Load(from)
	if err != nil {
		return false, err
	}
	if _, err = os.Stat(filepath.Join(from, "catalog.json")); errors.Is(err, fs.ErrNotExist) {
		return false, nil // nothing to move
	}
	if err = os.MkdirAll(filepath.Join(root, "videos"), 0700); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Join(from, "videos"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() || !safeID(e.Name()) {
			continue // staging left by an import cut short, or not ours
		}
		dst := filepath.Join(root, "videos", e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // moved by an earlier attempt
		}
		if err = os.Rename(filepath.Join(from, "videos", e.Name()), dst); err != nil {
			return false, err
		}
	}
	if err = AtomicJSON(filepath.Join(root, "catalog.json"), c); err != nil {
		return false, err
	}
	return true, os.Rename(filepath.Join(from, "catalog.json"), filepath.Join(from, "catalog.migrated.json"))
}
