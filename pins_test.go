package main

import (
	"context"
	"testing"

	"github.com/Lumiaqian/phosphene-go/internal/catalog"
	"github.com/Lumiaqian/phosphene-go/internal/platform"
)

// fakeHost stands in for the native module, counting the library notices.
type fakeHost struct{ notices int }

func (h *fakeHost) Probe(context.Context, string, string) (platform.MediaInfo, error) {
	return platform.MediaInfo{}, nil
}
func (h *fakeHost) Displays() ([]platform.DisplayInfo, error) { return nil, nil }
func (h *fakeHost) NotifyLibraryChanged() error               { h.notices++; return nil }
func (h *fakeHost) AgentStuckSignals() (int64, error)         { return 0, nil }

func TestPinDisplays(t *testing.T) {
	root, host := t.TempDir(), &fakeHost{}
	cfg := catalog.Default()
	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		cfg.Media = append(cfg.Media, catalog.MediaEntry{ID: id, Name: id[:4], Filename: "v.mp4", Width: 1920, Height: 1080, DurationSeconds: 10, FrameRate: 30})
		cfg.Playlist.VideoIDs = append(cfg.Playlist.VideoIDs, id)
	}
	a := &application{root: root, host: host, config: cfg,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建"}, {Identifier: "B", Name: "U2790B"}}}
	saved := func() catalog.Config {
		t.Helper()
		c, err := catalog.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := cfg.Media[0].ID, cfg.Media[1].ID

	a.pinDisplay("A", second)
	if p := saved().Playlist.Pins; len(p) != 1 || p["A"] != second {
		t.Fatalf("pin A: %v", p)
	}
	if got := a.pinnedOn(second); len(got) != 1 || got[0] != "内建" {
		t.Fatalf("pinnedOn: %v", got)
	}
	notices := host.notices
	a.pinDisplay("A", second) // unchanged: nothing saved
	if host.notices != notices {
		t.Fatal("an unchanged pin saved again")
	}
	a.pinAll(first)
	if p := saved().Playlist.Pins; p["A"] != first || p["B"] != first {
		t.Fatalf("pin all: %v", p)
	}
	a.pinDisplay("A", "")
	a.pinDisplay("B", "")
	if p := saved().Playlist.Pins; len(p) != 0 {
		t.Fatalf("unpinned displays still pinned: %v", p)
	}
	if a.status != "已保存，壁纸会自动更新" {
		t.Fatalf("status %q", a.status)
	}
}
