package catalog

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func copyLocal(in *os.File, path string, clone func(*os.File, string) error) error {
	if clone(in, path) == nil {
		return os.Chmod(path, 0600)
	}
	return writeVideo(path, func(w io.Writer) error { _, err := io.Copy(w, in); return err })
}

func digest(path string) (sum [sha256.Size]byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	h.Sum(sum[:0])
	return
}

// Serialize discovery and publication across importers. Each entry owns a
// directory link, so catalog rollback and deferred deletion stay independent.
func publishVideo(ctx context.Context, base, stage string, entry MediaEntry) error {
	dir, err := os.Open(base)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	dirs, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	video := filepath.Join(stage, entry.Filename)
	info, err := os.Stat(video)
	if err != nil {
		return err
	}
	var want [sha256.Size]byte
	hashed := false
discover:
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.IsDir() || !safeID(d.Name()) {
			continue
		}
		folder := filepath.Join(base, d.Name())
		files, err := os.ReadDir(folder)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, file := range files {
			switch strings.ToLower(filepath.Ext(file.Name())) {
			case ".mp4", ".m4v", ".mov":
			default:
				continue
			}
			candidate := filepath.Join(folder, file.Name())
			fi, err := os.Lstat(candidate)
			if err != nil || !fi.Mode().IsRegular() || fi.Size() != info.Size() {
				continue
			}
			if !hashed {
				want, err = digest(video)
				if err != nil {
					return err
				}
				hashed = true
			}
			got, err := digest(candidate)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if got != want {
				continue
			}
			link := filepath.Join(stage, ".reuse")
			if err = os.Link(candidate, link); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			if err = os.Rename(link, video); err != nil {
				return err
			}
			break discover
		}
	}
	return os.Rename(stage, filepath.Join(base, entry.ID))
}
