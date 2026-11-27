package catalog

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLocalAndConcurrentStreamReuse(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "original.mov")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	verify := func(p, s string) {
		t.Helper()
		b, e := os.ReadFile(p)
		if e != nil || string(b) != s {
			t.Fatalf("content %q, error %v", b, e)
		}
	}
	write(source, "first")
	probe := func(context.Context, string, string) (MediaEntry, error) {
		return MediaEntry{Width: 1, Height: 1, DurationSeconds: 1, FrameRate: 1}, nil
	}
	local, err := Import(context.Background(), root, source, probe)
	if err != nil {
		t.Fatal(err)
	}
	write(source, "other")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	original := VideoPath(root, local)
	verify(original, "first")
	stat := func(p string) os.FileInfo {
		t.Helper()
		i, e := os.Stat(p)
		if e != nil {
			t.Fatal(e)
		}
		return i
	}
	first := stat(original)
	type result struct {
		entry MediaEntry
		err   error
	}
	results := make(chan result, 4)
	for range 4 {
		go func() {
			m, e := ImportFrom(context.Background(), root, "shared", ".mp4", func(w io.Writer) error { _, e := io.WriteString(w, "other"); return e }, probe)
			results <- result{m, e}
		}()
	}
	var previous os.FileInfo
	ids := map[string]bool{}
	var last string
	for range 4 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		last = VideoPath(root, r.entry)
		i := stat(last)
		if ids[r.entry.ID] || os.SameFile(first, i) || (previous != nil && !os.SameFile(previous, i)) {
			t.Fatal("unexpected file or entry identity")
		}
		ids[r.entry.ID] = true
		previous = i
	}
	for id := range ids {
		dir := filepath.Join(root, "videos", id)
		if dir != filepath.Dir(last) {
			if e := os.RemoveAll(dir); e != nil {
				t.Fatal(e)
			}
		}
	}
	verify(last, "other")
	in, err := os.Open(last)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err = copyLocal(in, source, func(*os.File, string) error { return syscall.EXDEV }); err != nil {
		t.Fatal(err)
	}
	verify(source, "other")
}
