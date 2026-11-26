package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

func TestImportArgs(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		wd   string
		want []string
		ok   bool
	}{
		{"absolute", []string{"--import", "/a/b.mp4", "/c"}, "/wd", []string{"/a/b.mp4", "/c"}, true},
		{"relative against the second's directory", []string{"--import", "x.mov", "sub/y.mp4", "/abs.m4v"}, "/wd", []string{"/wd/x.mov", "/wd/sub/y.mp4", "/abs.m4v"}, true},
		{"relative without a directory", []string{"--import", "x.mov"}, "", []string{"x.mov"}, true},
		{"no paths", []string{"--import"}, "/wd", nil, false},
		{"no args", nil, "/wd", nil, false},
		{"another flag", []string{"--other", "x.mov"}, "/wd", nil, false},
		{"program name first", []string{"Loopwall", "--import", "x"}, "/wd", nil, false},
	} {
		got, ok := importArgs(c.args, c.wd)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: importArgs = %v, %v; want %v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func readResult(t *testing.T, root string) (int, []string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "import-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Imported int
		Errors   []string
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r.Imported, r.Errors
}

// A second instance carrying --import reaches the running app's import and
// leaves a result; it does not open a window.
func TestSecondInstanceImports(t *testing.T) {
	callbacks := importCallbacks(t)
	// Not ready: the result says so.
	a := &application{root: t.TempDir()}
	a.secondInstance([]string{"--import", "x.mp4"}, "/wd")
	if n, errs := readResult(t, a.root); n != 0 || len(errs) != 1 || !strings.Contains(errs[0], "尚未就绪") {
		t.Fatalf("not ready: %d %v", n, errs)
	}

	// Ready, nothing in the path to import: the result says so.
	dir := t.TempDir()
	touch(t, dir, "notes.txt")
	a = &application{root: t.TempDir(), libraryReady: true, host: &fakeHost{}, config: catalog.Default()}
	a.secondInstance([]string{"--import", "notes.txt"}, dir)
	(<-callbacks)()
	if n, errs := readResult(t, a.root); n != 0 || len(errs) != 1 || !strings.Contains(errs[0], "没有可导入的视频") {
		t.Fatalf("nothing to import: %d %v", n, errs)
	}

	// Importing: the result says so, instead of dropping the request.
	a.importing = true
	a.secondInstance([]string{"--import", "notes.txt"}, dir)
	if _, errs := readResult(t, a.root); len(errs) != 1 || !strings.Contains(errs[0], "请稍后") {
		t.Fatalf("busy: %v", errs)
	}
}
