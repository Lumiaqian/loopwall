package main

import (
	"slices"
	"strings"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func (a *application) settingsView(c *ui.Context) {
	pageFrame(c, "设置", "", nil, func(top float32) {
		scrollBody(c, top, 720, func() {
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconPalette, "外观")
				group(c, func() {
					groupRow(c, "模式", "", func() {
						mode := max(0, slices.Index(appearances, a.prefs.Appearance))
						if glassSegmented(c, &mode, "外观", false, "跟随系统", "浅色", "深色") {
							a.prefs.Appearance = appearances[mode]
							a.savePrefs()
						}
					})
					ui.Divider(c)
					groupRow(c, "强调色", "按钮、开关、选中项和角标的颜色。", func() { a.accentSwatches(c) })
				})
			})
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconDrop, "液态玻璃")
				group(c, func() {
					ui.Row(c).Gap(12).Padding(12).FillWidth().AlignItems(ui.Stretch).Children(func() {
						for i := range looks {
							a.lookCard(c, &looks[i])
						}
					})
				})
			})
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconSettings, "通用")
				group(c, func() {
					groupRow(c, "接电锁屏时保持亮屏", "解锁、拔掉电源或停用本墙纸时自动释放，不改变系统密码要求。", func() {
						keepAwake := a.config.Playlist.KeepAwakeWhenLockedOnAC
						if glassSwitch(c, &keepAwake, "接电锁屏保持亮屏", a.busy) {
							a.commit(func(cfg *catalog.Config) { cfg.Playlist.KeepAwakeWhenLockedOnAC = keepAwake })
						}
					})
					ui.Divider(c)
					groupRow(c, "省电模式", "用电池、开启低电量模式或机器过热时暂停壁纸，停在当前画面。", func() {
						powerSaving := a.config.Playlist.PowerSaving
						if glassSwitch(c, &powerSaving, "省电模式", a.busy) {
							a.commit(func(cfg *catalog.Config) { cfg.Playlist.PowerSaving = powerSaving })
						}
					})
					ui.Divider(c)
					groupRow(c, "登录时启动管理端", "常驻菜单栏，墙纸服务卡住时自动修复；完全退出也不影响壁纸播放。", func() {
						if glassSwitch(c, &a.login, "登录启动", false) {
							if e := mygo.App.SetOpenAtLogin(a.login); e != nil {
								a.status = e.Error()
								a.login = mygo.App.OpenAtLogin()
							}
						}
					})
					ui.Divider(c)
					groupRow(c, "软件更新", updateDetail(), func() {
						glassButton(c, nil, "检查更新", false).Disabled(!updatesUsed).OnClick(func() {
							if updatesUsed {
								updater.CheckForUpdates()
							}
						})
					})
				})
			})
			a.shareSettings(c)
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconFolder, "启用与资料库")
				group(c, func() {
					groupRow(c, "系统墙纸", "首次使用：在系统墙纸设置中选中「Loopwall 轮播」并应用到需要的显示器。", func() {
						glassButton(c, iconExternal, "打开", false).OnClick(openWallpaperSettings)
					})
					ui.Divider(c)
					groupRow(c, "资料库位置", shortPath(a.root), func() {
						glassButton(c, iconFolder, "在 Finder 中显示", false).OnClick(func() { _ = mygo.Shell.OpenPath(a.root) })
					})
				})
			})
			current.onBackdrop(c, ui.Text(c, strings.TrimSpace("Loopwall "+mygo.App.Version())+" · 基于 Phosphene v1.11.4 · MyGO v0.10.0 · MIT License").FontSize(11), true)
		})
	})
}

// accentSwatches lets the user pick the accent: a dot per color, the
// desktop's shown as a rainbow, the chosen one ringed.
func (a *application) accentSwatches(c *ui.Context) {
	t := c.Theme()
	for i, ac := range accents {
		chosen := ac.key == a.prefs.Accent || (a.prefs.Accent == "" && i == 0)
		dot := ui.ButtonBase(c.Key("accent-"+ac.key)).Size(22, 22).Radius(11).Label(ac.label).Tooltip(ac.label)
		if ac.color.A > 0 {
			dot.Background(ac.color)
		} else {
			dot.ConicGradient(ui.ConicGradient{Oklab: true, Stops: []ui.GradientStop{
				{Color: ui.Oklch(0.7, 0.2, 25), At: 0}, {Color: ui.Oklch(0.8, 0.17, 90), At: 0.25}, {Color: ui.Oklch(0.7, 0.17, 160), At: 0.5},
				{Color: ui.Oklch(0.6, 0.2, 270), At: 0.75}, {Color: ui.Oklch(0.7, 0.2, 25), At: 1},
			}})
		}
		if chosen {
			dot.Shadow(0, 0, 0, 2, t.Background).Shadow(0, 0, 0, 4, t.Text.Alpha(0.6))
		}
		if dot.Clicked() && !chosen {
			a.prefs.Accent = ac.key
			a.savePrefs()
		}
	}
}

// lookCard offers a look, as System Settings offers its appearances: a
// preview over the wallpaper now playing, its name, and what it does; the
// one chosen is ringed in the accent and checked.
func (a *application) lookCard(c *ui.Context, l *look) {
	t := c.Theme()
	chosen := l == current
	card := ui.ButtonBase(c.Key("look-"+l.label)).Grow(1).Width(0).Padding(6, 6, 10, 6).Radius(14).Gap(8).
		Label("液态玻璃：" + l.label)
	card.Column().AlignItems(ui.Stretch).Justify(ui.Start)
	if card.Hovered() && !chosen {
		card.Background(t.SurfaceHover)
	}
	card.Children(func() {
		preview := a.lookPreview(c, l)
		if chosen {
			preview.Shadow(0, 0, 0, 2, t.Accent)
		}
		ui.Column(c).Gap(2).Padding(0, 4).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				name := ui.Text(c, l.label).FontWeight(600)
				if chosen {
					name.TextColor(t.Accent)
					ui.Icon(c, iconCheck).FontSize(13).TextColor(t.Accent)
				}
			})
			ui.Text(c, l.about).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
		})
	})
	if card.Clicked() && !chosen {
		a.prefs.Glass = l.key
		a.savePrefs()
	}
}

// lookPreview draws a small window in look l, over the wallpaper now
// playing: a sidebar, two panes and a button in the accent.
func (a *application) lookPreview(c *ui.Context, l *look) ui.Element {
	t := c.Theme()
	return ui.Box(c).FillWidth().Height(96).Radius(10).Clip().Border(1, t.Text.Alpha(0.12)).Children(func() {
		a.backdrop(c, l, 0.25)
		l.fillPane(c, ui.Box(c).Absolute().Top(5).Left(5).Bottom(5).Width(34).Radius(9))
		l.fillContent(c, ui.Box(c).Absolute().Top(14).Left(46).Right(7).Height(30).Radius(8))
		l.fillContent(c, ui.Box(c).Absolute().Top(52).Left(46).Right(7).Height(30).Radius(8))
		l.fillControl(c, ui.Box(c).Absolute().Top(24).Right(16).Size(20, 9).Radius(5), glass.Glass{Tint: t.Accent})
	})
}
