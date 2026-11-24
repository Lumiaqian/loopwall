package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/mygo-ui-sdk/uitest"
	"github.com/egoist/mygo/ui"
)

func TestUITestPilotAgentReadyViews(t *testing.T) {
	a := &application{
		root:     t.TempDir(),
		host:     &fakeHost{},
		config:   catalog.Default(),
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建显示器"}},
	}
	a.config.Media = []catalog.MediaEntry{media(idOne)}

	cases := []struct {
		section string
		opts    uitest.AuditOptions
	}{
		{
			section: sectionLibrary,
			opts: uitest.AuditOptions{
				RequiredInteractive: []string{"导入视频", "搜索资料库", "筛选资料库：全部", "预览 " + media(idOne).Name},
				RequireFocusable:    true,
			},
		},
		{
			section: sectionPlay,
			opts: uitest.AuditOptions{
				RequiredInteractive: []string{"全部加入轮播", "前往资料库"},
				RequireFocusable:    true,
			},
		},
		{
			section: sectionNearby,
			opts: uitest.AuditOptions{
				RequireFocusable: true,
			},
		},
		{
			section: sectionSetting,
			opts: uitest.AuditOptions{
				RequiredInteractive: []string{"外观：跟随系统", "接电锁屏保持亮屏", "省电模式", "登录启动"},
				RequireFocusable:    true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			a.section = tc.section
			tt := ui.NewTester(a.view, 960, 720)
			uitest.AssertAgentReady(t, tt, tc.opts)
			uitest.AssertThemeParity(t, tt, tc.opts)
		})
	}
}

func TestAgentReadyInteractiveStates(t *testing.T) {
	m1, m2 := media(idOne), media(idTwo)
	m1.Name, m2.Name = "极光夜空", "深海波光"
	m1.DurationSeconds, m2.DurationSeconds = 30, 45

	t.Run("library_rename_and_multiple_cards", func(t *testing.T) {
		a := &application{
			root:     t.TempDir(),
			host:     &fakeHost{},
			config:   catalog.Default(),
			displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建显示器"}},
			section:  sectionLibrary,
		}
		a.config.Media = []catalog.MediaEntry{m1, m2}
		a.config.Playlist.VideoIDs = []string{m1.ID}
		a.startRename(m1)

		tt := ui.NewTester(a.view, 960, 720)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{
				"导入视频", "搜索资料库", "预览 极光夜空", "预览 深海波光",
				"视频名称", "保存名称", "取消",
			},
			RequireFocusable: true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)

		if info, ok := tt.Access("预览 极光夜空"); !ok || info.Role != ui.RoleButton || !info.Focusable {
			t.Fatalf("library preview button info=%+v ok=%v, want focusable RoleButton", info, ok)
		}
	})

	t.Run("playlist_duration_and_active_display", func(t *testing.T) {
		a := &application{
			root:     t.TempDir(),
			host:     &fakeHost{},
			config:   catalog.Default(),
			displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建显示器"}},
			section:  sectionPlay,
			interval: "60",
		}
		a.config.Media = []catalog.MediaEntry{m1, m2}
		a.config.Playlist.VideoIDs = []string{m1.ID, m2.ID}
		a.config.Playlist.AdvanceMode = "duration"
		a.config.Playlist.IntervalSeconds = 60
		a.runtime = catalog.Runtime{
			SchemaVersion:     1,
			UpdatedAtUnix:     float64(time.Now().Unix()),
			Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: m1.ID, PositionSeconds: 10}},
			ActiveDisplayKeys: []string{"A"},
		}

		tt := ui.NewTester(a.view, 960, 720)
		opts := uitest.AuditOptions{
			RequiredInteractive: []string{
				"系统墙纸设置", "预览 极光夜空", "切换时机：按时长",
				"轮播秒数", "30 秒", "1 分钟", "5 分钟", "15 分钟", "清空轮播",
			},
			AllowDuplicateNames: []string{"上移", "下移", "移出轮播"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, opts)
		uitest.AssertThemeParity(t, tt, opts)

		if info, ok := tt.Access("预览 极光夜空"); !ok || info.Role != ui.RoleButton || !info.Focusable {
			t.Fatalf("display card preview button info=%+v ok=%v, want focusable RoleButton", info, ok)
		}
	})
}

func TestAgentReadyPreviewWindow(t *testing.T) {
	realOpen := openPreviewVideo
	t.Cleanup(func() { openPreviewVideo = realOpen })

	m1, m2 := media(idOne), media(idTwo)
	m1.Name, m2.Name = "极光夜空", "深海波光"
	m1.DurationSeconds, m2.DurationSeconds = 20, 40

	app := &application{
		root:   t.TempDir(),
		config: catalog.Config{SchemaVersion: 1, Media: []catalog.MediaEntry{m1, m2}},
	}

	t.Run("ready_and_paused", func(t *testing.T) {
		fp := &fakePreviewPlayer{ready: true, playing: true, dur: 20 * time.Second, pos: 5 * time.Second}
		openPreviewVideo = func(string) (previewPlayer, *ui.VideoPlayer, error) {
			return fp, nil, nil
		}
		sess := &previewSession{app: app, renderVideo: func(*ui.Context) {}}
		sess.open(m1)

		tt := ui.NewTester(sess.view, 960, 580)
		tt.Move(480, 300)
		tt.Advance(time.Second)

		playingOpts := uitest.AuditOptions{
			RequiredInteractive: []string{"播放进度", "上一个", "暂停", "下一个"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, playingOpts)
		uitest.AssertThemeParity(t, tt, playingOpts)

		fp.playing = false
		tt.Frame()
		pausedOpts := uitest.AuditOptions{
			RequiredInteractive: []string{"播放进度", "上一个", "播放", "下一个"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, pausedOpts)
	})

	t.Run("loading_and_timeout_retry", func(t *testing.T) {
		openPreviewVideo = func(string) (previewPlayer, *ui.VideoPlayer, error) {
			return &fakePreviewPlayer{ready: false, dur: 10 * time.Second}, nil, nil
		}
		sess := &previewSession{app: app, renderVideo: func(*ui.Context) {}}
		sess.open(m1)

		tt := ui.NewTester(sess.view, 960, 580)
		uitest.AssertAgentReady(t, tt, uitest.AuditOptions{
			RequiredLabels:      []string{"正在加载视频"},
			RequiredInteractive: []string{"播放进度", "上一个", "播放", "下一个"},
			RequireFocusable:    true,
		})

		tt.Advance(previewFirstFrame + 50*time.Millisecond)
		timeoutOpts := uitest.AuditOptions{
			RequiredInteractive: []string{"重试", "上一个", "播放", "下一个"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, timeoutOpts)
		uitest.AssertThemeParity(t, tt, timeoutOpts)
	})

	t.Run("open_error_retry", func(t *testing.T) {
		openPreviewVideo = func(string) (previewPlayer, *ui.VideoPlayer, error) {
			return nil, nil, errors.New("decode failed")
		}
		sess := &previewSession{app: app, renderVideo: func(*ui.Context) {}}
		sess.open(m1)

		tt := ui.NewTester(sess.view, 960, 580)
		errOpts := uitest.AuditOptions{
			RequiredInteractive: []string{"重试", "上一个", "播放", "下一个"},
			RequireFocusable:    true,
		}
		uitest.AssertAgentReady(t, tt, errOpts)
		uitest.AssertThemeParity(t, tt, errOpts)
	})
}

func TestGlassNavTabSemanticsAndHomeEnd(t *testing.T) {
	section := "a"
	items := []navItem{
		{id: "a", icon: iconLibrary, label: "甲"},
		{id: "b", icon: iconPlaylist, label: "乙"},
		{id: "c", icon: iconSettings, label: "丙"},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		glassNav(c, &section, items)
	}, 240, 160)

	opts := uitest.AuditOptions{
		RequiredLabels:      []string{"分区"},
		RequiredInteractive: []string{"甲", "乙", "丙"},
		RequireFocusable:    true,
	}
	uitest.AssertAgentReady(t, tt, opts)

	if info, ok := tt.Access("分区"); !ok || info.Role != ui.RoleTabList {
		t.Fatalf("nav container info=%+v ok=%v, want RoleTabList", info, ok)
	}
	if info, ok := tt.Access("甲"); !ok || info.Role != ui.RoleTab || !info.Selected {
		t.Fatalf("selected nav tab info=%+v ok=%v, want selected RoleTab", info, ok)
	}

	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyEnd)
	tt.Advance(time.Second)
	if section != "c" {
		t.Fatalf("KeyEnd: section=%q, want c", section)
	}
	tt.Key(0, ui.KeyHome)
	tt.Advance(time.Second)
	if section != "a" {
		t.Fatalf("KeyHome: section=%q, want a", section)
	}
}

func TestGlassSegmentedHomeEndAndRTLViaSDK(t *testing.T) {
	sel := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(40).Children(func() {
			glassSegmented(c, &sel, "模式", false, "甲", "乙", "丙")
		})
	}, 360, 120)

	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyEnd)
	tt.Advance(time.Second)
	if sel != 2 {
		t.Fatalf("KeyEnd: sel=%d, want 2", sel)
	}
	tt.Key(0, ui.KeyHome)
	tt.Advance(time.Second)
	if sel != 0 {
		t.Fatalf("KeyHome: sel=%d, want 0", sel)
	}

	tt.SetDirection(ui.RightToLeft)
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	tt.Advance(time.Second)
	if sel != 1 {
		t.Fatalf("RTL KeyLeft: sel=%d, want 1", sel)
	}
}
