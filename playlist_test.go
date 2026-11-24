package main

import (
	"slices"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/ui"
)

const idThree = "33333333-3333-4333-8333-333333333333"

func TestMoveVideoID(t *testing.T) {
	base := []string{"a", "b", "c", "d"}
	if got := moveVideoID(base, 0, 2); !slices.Equal(got, []string{"b", "c", "a", "d"}) {
		t.Fatalf("forward 0->2 = %v", got)
	}
	if got := moveVideoID(base, 3, 1); !slices.Equal(got, []string{"a", "d", "b", "c"}) {
		t.Fatalf("backward 3->1 = %v", got)
	}
	if got := moveVideoID(base, 1, 1); !slices.Equal(got, base) {
		t.Fatalf("noop 1->1 = %v", got)
	}
	if got := moveVideoID(base, -1, 2); !slices.Equal(got, base) {
		t.Fatalf("out of bounds = %v", got)
	}
	if !slices.Equal(base, []string{"a", "b", "c", "d"}) {
		t.Fatal("moveVideoID mutated input slice")
	}
}

func TestPlaylistDragReorderAndBatchActions(t *testing.T) {
	m1, m2, m3 := media(idOne), media(idTwo), media(idThree)
	m1.Name, m2.Name, m3.Name = "视频一", "视频二", "视频三"
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m1, m2, m3}
	cfg.Playlist.VideoIDs = []string{idOne, idTwo, idThree}

	a := &application{
		root:    t.TempDir(),
		host:    &fakeHost{},
		config:  cfg,
		section: sectionPlay,
	}
	tt := ui.NewTester(a.view, 960, 760)

	r1, ok1 := tt.Find("视频一")
	r3, ok3 := tt.Find("视频三")
	if !ok1 || !ok3 {
		t.Fatalf("playlist rows not found: ok1=%v ok3=%v", ok1, ok3)
	}

	// Drag first row onto third row to reorder quietly without toast spam.
	tt.Press(r1.X+10, r1.Y+r1.H/2)
	for step := 1; step <= 4; step++ {
		f := float32(step) / 4
		tt.Move(r1.X+10, r1.Y+r1.H/2+(r3.Y+r3.H/2-(r1.Y+r1.H/2))*f)
	}
	tt.Release(r1.X+10, r3.Y+r3.H/2)

	want := []string{idTwo, idThree, idOne}
	if !slices.Equal(a.config.Playlist.VideoIDs, want) {
		t.Fatalf("after drag reorder = %v, want %v", a.config.Playlist.VideoIDs, want)
	}
	if a.status != "" {
		t.Fatalf("reorder produced unexpected toast status %q", a.status)
	}

	// Accessible step buttons also reorder quietly.
	if err := tt.Click("下移"); err != nil {
		t.Fatal(err)
	}
	want = []string{idThree, idTwo, idOne}
	if !slices.Equal(a.config.Playlist.VideoIDs, want) || a.status != "" {
		t.Fatalf("after step down: ids=%v status=%q", a.config.Playlist.VideoIDs, a.status)
	}

	// Clear playlist then add all back.
	if err := tt.Click("清空轮播"); err != nil {
		t.Fatal(err)
	}
	if len(a.config.Playlist.VideoIDs) != 0 {
		t.Fatalf("after clear: %v", a.config.Playlist.VideoIDs)
	}
	if err := tt.Click("全部加入轮播"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.config.Playlist.VideoIDs, []string{idOne, idTwo, idThree}) {
		t.Fatalf("after addAll: %v", a.config.Playlist.VideoIDs)
	}
}
