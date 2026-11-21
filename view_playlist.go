package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func (a *application) playlistView(c *ui.Context) {
	t := c.Theme()
	pageFrame(c, "播放与显示器", "首次均匀错开，之后每块屏独立续播", func() {
		glassButton(c, iconExternal, "系统墙纸设置", false).OnClick(openWallpaperSettings)
	}, func(top float32) {
		scrollBody(c, top, 0, func() {
			a.repairBanner(c)
			// Displays.
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconMonitor, "显示器")
				if len(a.displays) == 0 {
					ui.Text(c, "未检测到显示器").TextColor(t.TextMuted)
				}
				ui.Row(c).Wrap().Gap(14).FillWidth().Children(func() {
					for _, d := range a.displays {
						a.displayCard(c, d.Identifier, d.Name, d.BuiltIn)
					}
				})
				if a.runtimeFresh() {
					for _, e := range a.runtime.Errors {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 12).Radius(8).FillWidth().
							Background(t.Warning.Alpha(0.12)).TextColor(t.Warning).Children(func() {
							ui.Icon(c, iconAlert)
							ui.Text(c, e).FontSize(12).Grow(1)
						})
					}
				}
			})
			// Advance rule.
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconRepeat, "切换方式")
				group(c, func() {
					groupRow(c, "切换时机", "", func() {
						mode := 0
						if a.config.Playlist.AdvanceMode == "duration" {
							mode = 1
						}
						if glassSegmented(c, &mode, "切换时机", a.busy, "播完一遍", "按时长") {
							a.commit(func(cfg *catalog.Config) { cfg.Playlist.AdvanceMode = []string{"end", "duration"}[mode] })
						}
					})
					if a.config.Playlist.AdvanceMode == "duration" {
						ui.Divider(c)
						groupRow(c, "每项播放时长", "短于该时长的视频会自动循环", func() {
							ui.TextInput(c, &a.interval).Width(72).Radius(999).Label("轮播秒数").OnSubmit(a.applyInterval)
							ui.Text(c, "秒").TextColor(t.TextMuted)
							glassButton(c, nil, "应用", false).Disabled(a.busy || strings.TrimSpace(a.interval) == strconv.Itoa(a.config.Playlist.IntervalSeconds)).OnClick(a.applyInterval)
						})
					}
				})
			})
			// Order.
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				sectionTitle(c, iconList, fmt.Sprintf("播放顺序 · %d", len(a.config.Playlist.VideoIDs)))
				if len(a.config.Playlist.VideoIDs) == 0 {
					group(c, func() {
						ui.Column(c).Padding(22).Gap(4).Center().FillWidth().Children(func() {
							ui.Text(c, "列表为空").FontWeight(600)
							ui.Text(c, "壁纸会保留末帧。在资料库里点 + 加入视频。").FontSize(12).TextColor(t.TextMuted)
						})
					})
					return
				}
				group(c, func() {
					ids := a.config.Playlist.VideoIDs
					for i, id := range ids {
						m, ok := a.entry(id)
						if !ok {
							continue
						}
						if i > 0 {
							ui.Divider(c)
						}
						a.orderRow(c, i, len(ids), m)
					}
				})
			})
		})
	})
}

func (a *application) orderRow(c *ui.Context, i, n int, m catalog.MediaEntry) {
	t := c.Theme()
	row := ui.Row(c.Key(m.ID)).Gap(12).AlignItems(ui.Center).Padding(8, 12).FillWidth()
	hovered := row.Hovered()
	row.Children(func() {
		ui.Text(c, fmt.Sprintf("%02d", i+1)).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Width(20)
		thumbBox(c, a.thumb(m.ID), 64, 36, 5)
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, m.Name).FontWeight(600).SingleLine()
			ui.Text(c, fmt.Sprintf("%s · %s", clock(m.DurationSeconds), resolution(m.Width, m.Height))).FontSize(11).TextColor(t.TextMuted)
		})
		controls := ui.Row(c).Gap(2).Opacity(0.55)
		if hovered {
			controls.Opacity(1)
		}
		controls.Children(func() {
			iconButton(c, iconUp, "上移").Disabled(a.busy || i == 0).OnClick(func() {
				a.commit(func(c *catalog.Config) { ids := c.Playlist.VideoIDs; ids[i-1], ids[i] = ids[i], ids[i-1] })
			})
			iconButton(c, iconDown, "下移").Disabled(a.busy || i == n-1).OnClick(func() {
				a.commit(func(c *catalog.Config) { ids := c.Playlist.VideoIDs; ids[i+1], ids[i] = ids[i], ids[i+1] })
			})
			iconButton(c, iconX, "移出轮播").Disabled(a.busy).OnClick(func() { a.togglePlaylist(m.ID) })
		})
	})
}

func (a *application) displayCard(c *ui.Context, key, name string, builtIn bool) {
	t := c.Theme()
	s := a.displayState(key)
	current.fillContent(c, ui.Row(c.Key("disp-"+key)).Gap(14).Padding(12).Radius(16).Width(340).Grow(1)).Children(func() {
		var bm *ui.Bitmap
		if s.ok {
			bm = a.thumb(s.media.ID)
		}
		thumbBox(c, bm, 128, 72, 7)
		ui.Column(c).Gap(5).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconMonitor).TextColor(t.TextMuted)
				ui.Text(c, name).FontWeight(600).SingleLine().Shrink(1)
				if builtIn {
					ui.Badge(c, "内建")
				}
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Shrink(0).Background(s.color(t))
				ui.Text(c, s.title()).FontSize(12).SingleLine().Shrink(1)
			})
			if s.ok && s.held {
				ui.Text(c, "省电模式：已暂停，停在当前画面").FontSize(11).TextColor(t.TextMuted)
			}
			if s.ok && s.media.DurationSeconds > 0 {
				ui.Progress(c, math.Min(1, s.position/s.media.DurationSeconds)).Label("播放进度")
				ui.Text(c, fmt.Sprintf("%s / %s", clock(s.position), clock(s.media.DurationSeconds))).FontSize(11).TextColor(t.TextMuted)
			} else {
				ui.Text(c, s.detail()).FontSize(11).TextColor(t.TextMuted)
			}
			a.pinButton(c, key)
		})
	})
}

// pinButton chooses what display key plays: the playlist, or one video of
// the library, looped.
func (a *application) pinButton(c *ui.Context, key string) {
	pinned := a.config.Playlist.Pins[key]
	label := "播放：轮播"
	if m, ok := a.entry(pinned); ok {
		label = "固定：" + shorten(m.Name, 14)
	}
	ui.Row(c).Margin(4, 0, 0, 0).Children(func() {
		glassButton(c, iconPlaylist, label, false).Disabled(a.busy).Menu(func(menu *ui.Menu) {
			if menu.Item("轮播播放列表").Radio(pinned == "").Chosen() {
				a.pinDisplay(key, "")
			}
			menu.Separator()
			for _, m := range a.config.Media {
				if menu.Item(m.Name).ID("pin-" + m.ID).Radio(pinned == m.ID).Chosen() {
					a.pinDisplay(key, m.ID)
				}
			}
		})
	})
}

// pinDisplay makes display key loop video id alone, or play the playlist
// when id is empty.
func (a *application) pinDisplay(key, id string) {
	if a.busy || a.config.Playlist.Pins[key] == id {
		return
	}
	a.commit(func(c *catalog.Config) {
		if id == "" {
			delete(c.Playlist.Pins, key)
			return
		}
		if c.Playlist.Pins == nil {
			c.Playlist.Pins = map[string]string{}
		}
		c.Playlist.Pins[key] = id
	})
}

// pinAll makes every display loop video id alone, saving once.
func (a *application) pinAll(id string) {
	if a.busy || len(a.displays) == 0 {
		return
	}
	a.commit(func(c *catalog.Config) {
		if c.Playlist.Pins == nil {
			c.Playlist.Pins = map[string]string{}
		}
		for _, d := range a.displays {
			c.Playlist.Pins[d.Identifier] = id
		}
	})
}

// pinnedOn names the displays that loop video id alone.
func (a *application) pinnedOn(id string) []string {
	var names []string
	for _, d := range a.displays {
		if a.config.Playlist.Pins[d.Identifier] == id {
			names = append(names, d.Name)
		}
	}
	return names
}

func (a *application) applyInterval() {
	n, e := strconv.Atoi(strings.TrimSpace(a.interval))
	if e != nil || n < 1 {
		a.status = "请输入正整数秒"
		return
	}
	a.commit(func(c *catalog.Config) { c.Playlist.IntervalSeconds = n })
}

// repairBanner offers to rebuild WallpaperAgent while the wallpaper looks
// stuck (recovery.go).
func (a *application) repairBanner(c *ui.Context) {
	if !a.wallpaperStalled(time.Now()) {
		return
	}
	t := c.Theme()
	current.fillControl(c, ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(12, 14).Radius(14).FillWidth(),
		glass.Glass{Tint: t.Warning.Alpha(0.3)}).Children(func() {
		ui.Icon(c, iconAlert).FontSize(18).TextColor(t.Warning)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, "壁纸好像停了").FontWeight(600)
			ui.Text(c, "系统墙纸服务没有响应。修复会重建它，所有显示器的墙纸会闪一下；锁屏时出现属于正常，没在用本壁纸也可以忽略。").FontSize(11).TextColor(t.TextMuted)
		})
		glassButton(c, nil, "修复壁纸", true).OnClick(func() { a.repairWallpaper("正在重建墙纸服务…", time.Now()) })
	})
}
