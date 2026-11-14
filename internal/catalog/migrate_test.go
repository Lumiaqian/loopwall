package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrate(t *testing.T) {
	from, root := t.TempDir(), filepath.Join(t.TempDir(), "new")
	c := Default()
	m := MediaEntry{ID: "11111111-1111-4111-8111-111111111111", Name: "海", Filename: "video.mp4", Width: 1, Height: 1, DurationSeconds: 1, FrameRate: 1}
	c.Media = append(c.Media, m)
	c.Playlist.VideoIDs = []string{m.ID}
	c.Playlist.Pins = map[string]string{"display": m.ID}
	if err := Save(from, &c); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(from, "videos", m.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(VideoPath(from, m), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(from, "videos", ".import-123"), 0700); err != nil {
		t.Fatal(err)
	}

	moved, err := Migrate(from, root)
	if err != nil || !moved {
		t.Fatalf("moved %v: %v", moved, err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Media) != 1 || got.Playlist.Pins["display"] != m.ID || got.Revision != c.Revision {
		t.Errorf("migrated catalog: %+v", got)
	}
	if b, err := os.ReadFile(VideoPath(root, m)); err != nil || string(b) != "video" {
		t.Errorf("migrated video: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "videos", ".import-123")); err == nil {
		t.Error("moved an import's staging")
	}
	if _, err := os.Stat(filepath.Join(from, "catalog.json")); err == nil {
		t.Error("the old catalog is still there to move again")
	}
	// Once moved, never again; and a library of its own is never replaced.
	if moved, err = Migrate(from, root); err != nil || moved {
		t.Fatalf("again: moved %v: %v", moved, err)
	}
}

func TestMigrateNothing(t *testing.T) {
	root := t.TempDir()
	if moved, err := Migrate(filepath.Join(t.TempDir(), "absent"), root); err != nil || moved {
		t.Fatalf("no old library: moved %v: %v", moved, err)
	}
	// A library of its own stays as it is.
	from := t.TempDir()
	old, own := Default(), Default()
	old.Playlist.IntervalSeconds = 7
	if err := Save(from, &old); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, &own); err != nil {
		t.Fatal(err)
	}
	if moved, err := Migrate(from, root); err != nil || moved {
		t.Fatalf("over a library: moved %v: %v", moved, err)
	}
	if got, _ := Load(root); got.Playlist.IntervalSeconds != 60 {
		t.Error("replaced the library there")
	}
}

// TestMigrateResumes checks that a migration cut short after moving some
// videos picks up where it stopped.
func TestMigrateResumes(t *testing.T) {
	from, root := t.TempDir(), t.TempDir()
	c := Default()
	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		m := MediaEntry{ID: id, Name: id[:2], Filename: "video.mp4", Width: 1, Height: 1, DurationSeconds: 1, FrameRate: 1}
		c.Media = append(c.Media, m)
		if err := os.MkdirAll(filepath.Join(from, "videos", id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := Save(from, &c); err != nil {
		t.Fatal(err)
	}
	// The first moved, then it stopped.
	if err := os.MkdirAll(filepath.Join(root, "videos"), 0700); err != nil {
		t.Fatal(err)
	}
	first := c.Media[0].ID
	if err := os.Rename(filepath.Join(from, "videos", first), filepath.Join(root, "videos", first)); err != nil {
		t.Fatal(err)
	}
	if moved, err := Migrate(from, root); err != nil || !moved {
		t.Fatalf("resumed: moved %v: %v", moved, err)
	}
	for _, m := range c.Media {
		if _, err := os.Stat(filepath.Join(root, "videos", m.ID)); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
	}
}
