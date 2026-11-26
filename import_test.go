package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/catalog"
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
func importCallbacks(t *testing.T) <-chan func() {
	t.Helper()
	callbacks := make(chan func(), 1)
	original := importOnMain
	importOnMain = func(f func()) { callbacks <- f }
	t.Cleanup(func() { importOnMain = original })
	return callbacks
}

func TestDropImports(t *testing.T) {
	callbacks := importCallbacks(t)
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
	if !a.importing {
		t.Fatal("scan did not reserve the import slot")
	}
	(<-callbacks)()
	if a.importing {
		t.Fatal("empty scan retained the import slot")
	}
	tt.Frame()
	if !strings.Contains(status, "没有可导入的视频") {
		t.Errorf("after dropping a text file: %q", status)
	}
	a.importing = true
	tt.DropFiles(600, 400, filepath.Join(dir, "notes.txt"))
	tt.Frame()
	if !strings.Contains(status, "请稍后") {
		t.Errorf("dropped while importing: %q", status)
	}
}

func TestChooseImportSilentCancelAndBatchProgress(t *testing.T) {
	a := &application{root: t.TempDir(), host: &fakeHost{}, config: catalog.Default()}
	a.finishChooseImport(nil, nil)
	if a.status != "" || a.choosingImport {
		t.Fatalf("cancel set unexpected status=%q choosing=%v", a.status, a.choosingImport)
	}

	a.importing = true
	a.importDone = 1
	a.importTotal = 3
	a.importCurrent = "sunset.mp4"
	tt := ui.NewTester(a.view, 900, 700)
	if _, ok := tt.Find("正在导入第 2 / 3 个…"); !ok {
		t.Fatal("batch import progress banner not shown")
	}
	if _, ok := tt.Find("sunset.mp4"); !ok {
		t.Fatal("current importing file name not shown")
	}
	if _, ok := tt.Find("导入进度"); !ok {
		t.Fatal("import progress bar not shown")
	}
}

func TestImportFilesProgress(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.mp4", "b.mp4")
	a := &application{root: t.TempDir(), host: &fakeHost{}, config: catalog.Default()}
	var steps []string
	a.importFiles([]string{filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")}, func(done, total int, name string) {
		steps = append(steps, filepath.Base(name)+":"+string(rune('0'+done))+"/"+string(rune('0'+total)))
	})
	want := []string{"a.mp4:0/2", "a.mp4:1/2", "b.mp4:1/2", "b.mp4:2/2"}
	if !slices.Equal(steps, want) {
		t.Fatalf("progress steps = %v, want %v", steps, want)
	}
}
