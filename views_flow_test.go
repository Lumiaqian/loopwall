package main

import (
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestFirstEnableClosedLoopAndSidebarNavigation(t *testing.T) {
	restarts, real := 0, restartAgent
	restartAgent = func() error { restarts++; return nil }
	t.Cleanup(func() { restartAgent = real })

	// 1. Empty library shows CTA button; empty playlist CTA jumps back to library.
	a := &application{
		root:     t.TempDir(),
		host:     &fakeHost{},
		config:   catalog.Default(),
		section:  sectionLibrary,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建显示器"}},
	}
	tt := ui.NewTester(a.view, 960, 720)
	if _, ok := tt.Find("导入视频"); !ok {
		t.Fatal("empty library missing 导入视频 CTA")
	}

	a.section = sectionPlay
	tt.Frame()
	if err := tt.Click("前往资料库"); err != nil {
		t.Fatal(err)
	}
	if a.section != sectionLibrary {
		t.Fatalf("section after empty playlist CTA = %q, want %q", a.section, sectionLibrary)
	}

	// 2. Clicking a sidebar display item jumps to playlist & displays.
	if err := tt.Click("内建显示器：未启用"); err != nil {
		t.Fatal(err)
	}
	if a.section != sectionPlay {
		t.Fatalf("section after clicking sidebar display = %q, want %q", a.section, sectionPlay)
	}

	// 3. Library with videos and inactive displays shows enable guide banner,
	// which disappears once a display becomes active.
	m := media(idOne)
	a.config.Media = []catalog.MediaEntry{m}
	a.section = sectionLibrary
	tt.Frame()
	if _, ok := tt.Find("尚未在显示器启用「Loopwall 轮播」，前往系统墙纸设置选择即可开始播放"); !ok {
		t.Fatal("missing enable wallpaper guide banner")
	}

	a.runtime = catalog.Runtime{
		SchemaVersion:     1,
		UpdatedAtUnix:     float64(time.Now().Unix()),
		Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: idOne}},
		ActiveDisplayKeys: []string{"A"},
	}
	tt.Frame()
	if _, ok := tt.Find("尚未在显示器启用「Loopwall 轮播」，前往系统墙纸设置选择即可开始播放"); ok {
		t.Fatal("guide banner stayed visible while display is active")
	}

	// 4. Stalled wallpaper offers non-glass repair row in sidebar and triggers repair.
	a.stallSince = time.Now().Add(-stallAfter - 5*time.Second)
	tt.Frame()
	if err := tt.Click("修复壁纸"); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d, want 1", restarts)
	}
}
