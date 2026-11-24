package main

import (
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
				RequiredInteractive: []string{"导入视频", "搜索资料库", "筛选资料库：全部"},
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
