package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// touch makes the files rel under dir.
func touch(t *testing.T, dir string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		p := filepath.Join(dir, r)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVideoFiles(t *testing.T) {
	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	touch(t, dir, "a.mp4", "notes.txt", "folder/b.MOV", "folder/sub/c.m4v", "folder/cover.jpg",
		"folder/.hidden.mp4", "folder/.cache/d.mp4", "library/videos/x/video.mp4")
	got, skipped := videoFiles([]string{
		filepath.Join(dir, "a.mp4"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, "folder"),
		filepath.Join(dir, "a.mp4"), // twice: once
		filepath.Join(dir, "gone.mp4"),
		library,
	}, library)
	var rel []string
	for _, p := range got {
		r, _ := filepath.Rel(dir, p)
		rel = append(rel, r)
	}
	want := []string{"a.mp4", "folder/b.MOV", "folder/sub/c.m4v"}
	if !slices.Equal(rel, want) {
		t.Errorf("videos %v, want %v", rel, want)
	}
	// notes.txt, cover.jpg and the missing file.
	if skipped != 3 {
		t.Errorf("skipped %d, want 3", skipped)
	}
}

// TestDropImports checks that files dropped anywhere on the window go to
// the import: none of them a video, it says so; while one runs, it waits.
func TestDropImports(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "notes.txt")
	a := &application{root: t.TempDir(), host: &fakeHost{}, section: sectionSetting}
	var status string
	tt := ui.NewTester(func(c *ui.Context) {
		if a.status != "" {
			status = a.status
		}
		a.view(c)
	}, 1000, 700)
	if !tt.DropFiles(600, 400, filepath.Join(dir, "notes.txt")) {
		t.Fatal("the window did not take the files")
	}
	tt.Frame()
	if !strings.Contains(status, "没有可导入的视频") {
		t.Errorf("after dropping a text file: %q", status)
	}
	a.busy = true
	tt.DropFiles(600, 400, filepath.Join(dir, "notes.txt"))
	tt.Frame()
	if !strings.Contains(status, "请稍后") {
		t.Errorf("dropped while busy: %q", status)
	}
}
