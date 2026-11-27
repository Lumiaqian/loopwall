package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestFilterLibraryMedia(t *testing.T) {
	m1 := media(idOne)
	m1.Name = "Forest Dawn"
	m1.Width, m1.Height = 3840, 2160
	m2 := media(idTwo)
	m2.Name = "Ocean Waves"
	m2.Width, m2.Height = 1920, 1080
	m3 := media(idThree)
	m3.Name = "Night City"

	all := []catalog.MediaEntry{m1, m2, m3}
	playlist := []string{idOne, idThree}

	if got := filterLibraryMedia(all, playlist, "", libraryFilterAll); len(got) != 3 {
		t.Fatalf("all filter len = %d", len(got))
	}
	if got := filterLibraryMedia(all, playlist, "", libraryFilterInPlaylist); len(got) != 2 || got[0].ID != idOne || got[1].ID != idThree {
		t.Fatalf("inPlaylist = %+v", got)
	}
	if got := filterLibraryMedia(all, playlist, "", libraryFilterNotInPlaylist); len(got) != 1 || got[0].ID != idTwo {
		t.Fatalf("notInPlaylist = %+v", got)
	}
	if got := filterLibraryMedia(all, playlist, "ocean", libraryFilterAll); len(got) != 1 || got[0].ID != idTwo {
		t.Fatalf("query ocean = %+v", got)
	}
	if got := filterLibraryMedia(all, playlist, "4k", libraryFilterAll); len(got) != 1 || got[0].ID != idOne {
		t.Fatalf("query 4k = %+v", got)
	}
}

func TestMediaSizeCachingAndDeleteCleanup(t *testing.T) {
	root := t.TempDir()
	m := media(idOne)
	p := catalog.VideoPath(root, m)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, 2_500_000), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &application{root: root}
	if got := a.mediaSize(m); got != 2_500_000 {
		t.Fatalf("first mediaSize = %d, want 2500000", got)
	}
	// Cached even if underlying file is removed.
	_ = os.Remove(p)
	if got := a.mediaSize(m); got != 2_500_000 {
		t.Fatalf("cached mediaSize = %d, want 2500000", got)
	}
}

func TestLibraryRenameContextMenuSearchAndPlayingBadge(t *testing.T) {
	root := t.TempDir()
	m1 := media(idOne)
	m1.Name = "林间清晨"
	m1.Width, m1.Height = 3840, 2160
	m2 := media(idTwo)
	m2.Name = "深海波光"

	p1 := catalog.VideoPath(root, m1)
	if err := os.MkdirAll(filepath.Dir(p1), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, make([]byte, 1_200_000), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m1, m2}
	cfg.Playlist.VideoIDs = []string{idOne}

	a := &application{
		root:     root,
		host:     &fakeHost{},
		config:   cfg,
		section:  sectionLibrary,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建"}},
		runtime: catalog.Runtime{
			SchemaVersion:     1,
			UpdatedAtUnix:     float64(time.Now().Unix()),
			Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: idOne}},
			ActiveDisplayKeys: []string{"A"},
		},
	}
	tt := ui.NewTester(a.view, 900, 720)

	// Subtitle includes total size, card shows file size and playing badge.
	if _, ok := tt.Find("2 个视频 · 1 个在轮播中 · 共 1 MB · 原画质，静音播放 · 当前主题：默认主题"); !ok {
		t.Fatal("subtitle missing total file size")
	}
	if _, ok := tt.Find("4K · 30 fps · 1 MB"); !ok {
		t.Fatal("card meta missing file size")
	}
	if _, ok := tt.Find("播放中"); !ok {
		t.Fatal("playing badge missing on active video card")
	}

	// Double-click title enters rename, auto-focuses input, and Esc cancels.
	rMeta, ok := tt.Find("4K · 30 fps · 1 MB")
	if !ok {
		t.Fatal("video meta not found")
	}
	tt.ClickAt(rMeta.X+10, rMeta.Y-8)
	tt.ClickAt(rMeta.X+10, rMeta.Y-8)
	if a.selected != idOne {
		t.Fatalf("double click did not enter rename: selected=%q", a.selected)
	}
	if !tt.Focused("视频名称") {
		t.Fatal("rename input was not auto-focused")
	}
	tt.Key(0, ui.KeyEscape)
	if a.selected != "" {
		t.Fatalf("Escape did not cancel rename: selected=%q", a.selected)
	}

	// Narrow window check: toolbar filter fits inside 760px width.
	ttNarrow := ui.NewTester(a.view, 760, 640)
	if r, ok := ttNarrow.Find("筛选资料库：全部"); !ok || r.X+r.W > 760 {
		t.Fatalf("toolbar filter overflowed 760px window: ok=%v rect=%+v", ok, r)
	}

	// Right-click card opens context menu and can toggle playlist.
	if err := tt.RightClick("预览 深海波光"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("加入轮播"); err != nil {
		t.Fatal(err)
	}
	if len(a.config.Playlist.VideoIDs) != 2 {
		t.Fatalf("context menu did not add to playlist: %v", a.config.Playlist.VideoIDs)
	}

	// Search query narrows cards and shows empty state with reset button.
	a.libraryQuery = "不存在的片段"
	tt.Frame()
	if _, ok := tt.Find("没有匹配的视频"); !ok {
		t.Fatal("filtered empty state not shown")
	}
	if err := tt.Click("重置筛选"); err != nil {
		t.Fatal(err)
	}
	if a.libraryQuery != "" {
		t.Fatalf("reset filter did not clear query: %q", a.libraryQuery)
	}
	if _, ok := tt.Find("深海波光"); !ok {
		t.Fatal("video card did not reappear after reset")
	}
}
