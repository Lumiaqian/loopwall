package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/ui"
)

var intervalPresets = []struct {
	label   string
	seconds int
}{
	{"30 秒", 30},
	{"1 分钟", 60},
	{"5 分钟", 300},
	{"15 分钟", 900},
}

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
						if glassSegmented(c, &mode, "切换时机", false, "播完一遍", "按时长") {
							a.commit(func(cfg *catalog.Config) { cfg.Playlist.AdvanceMode = []string{"end", "duration"}[mode] })
						}
					})
					if a.config.Playlist.AdvanceMode == "duration" {
						ui.Divider(c)
						groupRow(c, "每项播放时长", "短于该时长的视频会自动循环", func() {
							ui.TextInput(c, &a.interval).Width(72).Radius(999).Label("轮播秒数").OnSubmit(a.applyInterval)
							ui.Text(c, "秒").TextColor(t.TextMuted)
							glassButton(c, nil, "应用", false).Disabled(strings.TrimSpace(a.interval) == strconv.Itoa(a.config.Playlist.IntervalSeconds)).OnClick(a.applyInterval)
						})
						ui.Divider(c)
						groupRow(c, "常用时长", "", func() {
							for _, preset := range intervalPresets {
								active := a.config.Playlist.IntervalSeconds == preset.seconds
								b := ui.ButtonBase(c.Key("preset-"+strconv.Itoa(preset.seconds))).
									Padding(5, 12).Radius(999).Label(preset.label)
								switch {
								case active:
									b.Background(t.Accent.Alpha(0.16)).TextColor(t.Accent)
								case b.Pressed():
									b.Background(t.SurfacePressed).TextColor(t.Text)
								case b.Hovered():
									b.Background(t.SurfaceHover).TextColor(t.Text)
								default:
									b.Background(t.Surface).TextColor(t.TextMuted)
								}
								b.OnClick(func() {
									a.interval = strconv.Itoa(preset.seconds)
									a.applyInterval()
								})
								b.Children(func() {
									ui.Text(c, preset.label).FontSize(12).FontWeight(600).SingleLine()
								})
							}
						})
					}
				})
			})
			// Order.
			ui.Column(c).Gap(10).FillWidth().Children(func() {
				hasMissing := len(a.missingPlaylistIDs()) > 0
				ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
					sectionTitle(c, iconList, fmt.Sprintf("播放顺序 · %d", len(a.config.Playlist.VideoIDs)))
					ui.Box(c).Grow(1)
					if len(a.config.Playlist.VideoIDs) > 0 && hasMissing {
						glassButton(c, iconPlus, "全部加入轮播", false).OnClick(a.addAllToPlaylist)
					}
					if len(a.config.Playlist.VideoIDs) > 0 {
						glassButton(c, nil, "清空轮播", false).OnClick(a.clearPlaylist)
					}
				})
				if len(a.config.Playlist.VideoIDs) == 0 {
					group(c, func() {
						ui.Column(c).Padding(22).Gap(8).Center().FillWidth().Children(func() {
							ui.Text(c, "列表为空").FontWeight(600)
							ui.Text(c, "壁纸会保留末帧，前往资料库选择视频加入轮播。").FontSize(12).TextColor(t.TextMuted)
							ui.Row(c).Gap(8).Center().Margin(4, 0, 0, 0).Children(func() {
								if len(a.config.Media) > 0 {
									glassButton(c, iconPlus, "全部加入轮播", false).OnClick(a.addAllToPlaylist)
								}
								glassButton(c, iconLibrary, "前往资料库", false).OnClick(func() { a.section = sectionLibrary })
							})
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

type playlistDrag struct {
	id   string
	from int
}

// moveVideoID returns a copy of ids with element from moved to index to.
func moveVideoID(ids []string, from, to int) []string {
	out := slices.Clone(ids)
	if from < 0 || from >= len(out) || to < 0 || to >= len(out) || from == to {
		return out
	}
	item := out[from]
	out = slices.Delete(out, from, from+1)
	out = slices.Insert(out, to, item)
	return out
}

func (a *application) reorderPlaylist(from, to int) {
	if from < 0 || from >= len(a.config.Playlist.VideoIDs) || to < 0 || to >= len(a.config.Playlist.VideoIDs) || from == to {
		return
	}
	a.commitQuiet(func(c *catalog.Config) {
		c.Playlist.VideoIDs = moveVideoID(c.Playlist.VideoIDs, from, to)
	})
}

func (a *application) missingPlaylistIDs() []string {
	var missing []string
	for _, m := range a.config.Media {
		if !slices.Contains(a.config.Playlist.VideoIDs, m.ID) {
			missing = append(missing, m.ID)
		}
	}
	return missing
}

func (a *application) addAllToPlaylist() {
	missing := a.missingPlaylistIDs()
	if len(missing) == 0 {
		return
	}
	if a.commit(func(c *catalog.Config) {
		for _, id := range missing {
			if !slices.Contains(c.Playlist.VideoIDs, id) {
				c.Playlist.VideoIDs = append(c.Playlist.VideoIDs, id)
			}
		}
	}) {
		a.status = fmt.Sprintf("已将 %d 个视频加入轮播", len(missing))
	}
}

func (a *application) clearPlaylist() {
	if len(a.config.Playlist.VideoIDs) == 0 {
		return
	}
	if a.commit(func(c *catalog.Config) { c.Playlist.VideoIDs = []string{} }) {
		a.status = "已清空轮播列表"
	}
}

func (a *application) orderRow(c *ui.Context, i, n int, m catalog.MediaEntry) {
	t := c.Theme()
	row := ui.Row(c.Key(m.ID)).Gap(12).AlignItems(ui.Center).Padding(8, 12).FillWidth().
		Drag(playlistDrag{id: m.ID, from: i})
	if drag, ok := ui.Drop[playlistDrag](row); ok && drag.from != i {
		a.reorderPlaylist(drag.from, i)
	}
	drag, over := ui.DragOver[playlistDrag](row)
	hovered := row.Hovered()
	if row.Dragging() {
		row.Opacity(0.45)
	} else if over && drag.from != i {
		row.Background(t.Accent.Alpha(0.06))
	}
	row.Children(func() {
		if over && drag.from != i {
			line := ui.Box(c).Absolute().Left(12).Right(12).Height(2).Background(t.Accent)
			if drag.from > i {
				line.Top(0)
			} else {
				line.Bottom(0)
			}
		}
		ui.Icon(c, iconGrip).FontSize(14).TextColor(t.TextMuted).Cursor(ui.CursorGrab)
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
			iconButton(c, iconUp, "上移").Disabled(i == 0).OnClick(func() { a.reorderPlaylist(i, i-1) })
			iconButton(c, iconDown, "下移").Disabled(i == n-1).OnClick(func() { a.reorderPlaylist(i, i+1) })
			iconButton(c, iconX, "移出轮播").OnClick(func() { a.togglePlaylist(m.ID) })
		})
	})
}

func (a *application) displayCard(c *ui.Context, key, name string, builtIn bool) {
	t := c.Theme()
	s := a.displayStateAt(key, c.Now())
	if s.ok && !s.held && !s.waiting && s.media.DurationSeconds > 0 {
		c.After(time.Second)
	}
	current.fillContent(c, ui.Row(c.Key("disp-"+key)).Gap(14).Padding(12).Radius(16).Width(340).MaxWidth(480).Grow(1)).Children(func() {
		var bm *ui.Bitmap
		if s.ok {
			bm = a.thumb(s.media.ID)
		}
		if s.ok {
			th := ui.ButtonBase(c.Key("disp-preview-"+key)).Size(128, 72).Shrink(0).Radius(7).Clip().
				Cursor(ui.CursorPointer).Label("预览 " + s.media.Name).Tooltip("预览 " + s.media.Name)
			th.Children(func() {
				thumbBox(c, bm, 128, 72, 7)
			})
			if th.Clicked() {
				a.previewMedia(s.media)
			}
		} else {
			thumbBox(c, bm, 128, 72, 7)
		}
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
		glassButton(c, iconPlaylist, label, false).Menu(func(menu *ui.Menu) {
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
	if a.config.Playlist.Pins[key] == id {
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
	if len(a.displays) == 0 {
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
	current.fillContent(c, ui.Box(c).Radius(14).Clip().FillWidth()).Border(1, t.Warning.Alpha(0.35)).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(12, 14).FillWidth().Background(t.Warning.Alpha(0.14)).Children(func() {
			ui.Icon(c, iconAlert).FontSize(18).TextColor(t.Warning)
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, "壁纸好像停了").FontWeight(600)
				ui.Text(c, "系统墙纸服务没有响应。修复会重建它，所有显示器的墙纸会闪一下；锁屏时出现属于正常，没在用本壁纸也可以忽略。").FontSize(11).TextColor(t.TextMuted)
			})
			glassButton(c, nil, "修复壁纸", true).OnClick(func() { a.repairWallpaper("正在重建墙纸服务…", time.Now()) })
		})
	})
}
