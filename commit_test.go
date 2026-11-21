package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

const (
	idOne = "11111111-1111-4111-8111-111111111111"
	idTwo = "22222222-2222-4222-8222-222222222222"
)

func media(id string) catalog.MediaEntry {
	return catalog.MediaEntry{ID: id, Name: id[:4], Filename: "video.mp4", Width: 1920, Height: 1080, DurationSeconds: 10, FrameRate: 30}
}

// failSaves makes every save fail until the test ends.
func failSaves(t *testing.T) {
	t.Helper()
	real := saveCatalog
	saveCatalog = func(string, *catalog.Config) error { return errors.New("disk full") }
	t.Cleanup(func() { saveCatalog = real })
}

func TestFailedSaveKeepsConfig(t *testing.T) {
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{media(idOne), media(idTwo)}
	cfg.Playlist.VideoIDs = []string{idOne}
	cfg.Playlist.Pins = map[string]string{"A": idOne}
	a := &application{root: t.TempDir(), host: &fakeHost{}, config: cfg}
	want := cloneConfig(a.config)
	failSaves(t)

	a.togglePlaylist(idTwo)
	a.pinDisplay("B", idTwo)
	a.commitRename(idOne) // empty name: nothing to do
	a.rename = "renamed"
	a.commitRename(idOne)
	if !reflect.DeepEqual(a.config, want) {
		t.Fatalf("a failed save changed the config:\n got %+v\nwant %+v", a.config, want)
	}
	if a.status != "保存失败：disk full" {
		t.Fatalf("status %q", a.status)
	}
}

func TestFailedImportRemovesPublishedCopies(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(src, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &application{root: root, host: &fakeHost{}, config: catalog.Default()}
	probe := func(_ context.Context, _, thumb string) (catalog.MediaEntry, error) {
		return media(idOne), os.WriteFile(thumb, []byte("jpg"), 0o600)
	}
	m, err := catalog.ImportFrom(context.Background(), root, "clip", ".mp4", func(w io.Writer) error {
		_, err := w.Write([]byte("video"))
		return err
	}, probe)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "videos", m.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	failSaves(t)
	want := cloneConfig(a.config)
	if a.finish(a.addImported([]catalog.MediaEntry{m})) {
		t.Fatal("import reported saved")
	}
	if !reflect.DeepEqual(a.config, want) {
		t.Fatalf("config changed: %+v", a.config)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("orphaned copy left behind: %v", err)
	}
}

func TestSuccessfulCommitAdopts(t *testing.T) {
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{media(idOne)}
	a := &application{root: t.TempDir(), host: &fakeHost{}, config: cfg}
	if !a.commit(func(c *catalog.Config) { c.Playlist.VideoIDs = append(c.Playlist.VideoIDs, idOne) }) {
		t.Fatal(a.status)
	}
	if len(a.config.Playlist.VideoIDs) != 1 || a.config.Revision != 1 {
		t.Fatalf("config %+v", a.config)
	}
	if len(cfg.Playlist.VideoIDs) != 0 {
		t.Fatal("the original's slice was edited")
	}
}
