package main

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

const (
	titleBarHeight = 52
	headerHeight   = 72
	sidebarWidth   = 220
	sectionLibrary = "library"
	sectionPlay    = "playlist"
	sectionSetting = "settings"
	sectionNearby  = "nearby"
)

func (a *application) view(c *ui.Context) {
	current = lookFor(a.prefs.Glass)
	t := a.prefs.theme(c.Theme(), c.Preferences())
	t.Radius = 7
	// Everything sits on glass: control faces, hovers, selections and lines
	// are tints of the text color, so what is under them shows through.
	t.Surface, t.SurfaceHover, t.SurfacePressed = t.Text.Alpha(0.06), t.Text.Alpha(0.08), t.Text.Alpha(0.12)
	t.Border = t.Text.Alpha(0.12)
	c.SetTheme(&t)
	if a.status != "" {
		c.Toast(a.status)
		a.status = ""
	}
	// The whole window takes videos and folders dragged from Finder.
	root := ui.Box(c).Fill().Clip()
	if files := root.DroppedFiles(); files != nil {
		a.importPaths(files)
	}
	var over float32
	if root.FileDragOver() {
		over = 1
	}
	hint := root.AnimateTo("drop-hint", over, ui.Smooth)
	root.Children(func() {
		a.backdrop(c, current, 1)
		overlay(ui.Row(c)).AlignItems(ui.Stretch).Children(func() {
			// The sidebar floats as a pane of Liquid Glass over the backdrop.
			current.fillPane(c, ui.Column(c).Width(sidebarWidth).Shrink(0).Margin(8, 0, 8, 8).Radius(18)).
				Children(func() { a.sidebar(c) })
			ui.Box(c).Grow(1).MinWidth(0).Children(func() {
				switch a.section {
				case sectionPlay:
					a.playlistView(c)
				case sectionSetting:
					a.settingsView(c)
				case sectionNearby:
					a.nearbyView(c)
				default:
					a.libraryView(c)
				}
			})
		})
		dropHint(c, hint)
		glassToasts(c)
	})
}

// dropHint shows, as files are dragged over the window, that letting go
// imports them: the window dims and a pane of glass says so, by k, from 0
// to 1 as the files come over it.
func dropHint(c *ui.Context, k float32) {
	t := c.Theme()
	if k < 0.01 {
		return
	}
	overlay(ui.Box(c)).PassThrough().Background(ui.RGBA(0, 0, 0, 0.22*k)).Center().Children(func() {
		pane := ui.Column(c).Gap(10).Padding(28, 40).Radius(28).AlignItems(ui.Center).Opacity(k).Scale(0.94+0.06*k, 0.94+0.06*k)
		current.fillControl(c, pane, glass.Glass{}).InsetShadow(0, 0, 0, 2, t.Accent.Alpha(0.6)).Children(func() {
			ui.Icon(c, iconImport).FontSize(34).TextColor(t.Accent)
			ui.Text(c, "松开以导入到资料库").FontSize(17).Bold()
			ui.Text(c, "支持 MP4、MOV、M4V；文件夹会导入其中的全部视频").FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// glassToasts draws the window's toasts as capsules of glass rising from
// the bottom of the content, after it so the glass shows it through.
func glassToasts(c *ui.Context) {
	t := c.Theme()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(0, 0, 28, sidebarWidth+8).AlignItems(ui.Center).Gap(8)
		for _, tt := range toasts {
			toast := ui.ToastBase(c, tt)
			current.fillControl(c, toast.Root.Padding(10, 20).Radius(999), glass.Glass{}).
				Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 12}, Exit: &ui.Motion{Y: 12}})
			toast.Root.Children(func() {
				ui.Text(c, tt.Title).FontWeight(600).TextColor(t.Text)
			})
		}
	})
}

// backdrop fills its parent with the wallpaper now playing, blurred and
// toned as l says, for the glass to show through; scale shrinks the blur
// for a preview. Without glass it is the plain background.
func (a *application) backdrop(c *ui.Context, l *look, scale float32) {
	t := c.Theme()
	if !l.glass {
		overlay(ui.Box(c)).Background(solidBackground(t))
		return
	}
	if bm := a.thumbs[a.ambientID()]; bm != nil {
		// Overscan so the blur does not fade in at the edges.
		over := 2 * l.blur * scale
		ui.Image(c, bm).Absolute().Top(-over).Left(-over).Right(-over).Bottom(-over).Fit(ui.Cover).Blur(l.blur * scale).Cache()
	} else {
		overlay(ui.Box(c)).Gradient(t.Accent.Alpha(0.35), t.Background, 135)
	}
	overlay(ui.Box(c)).Background(l.backdropTone(t))
}

// ambientID is the video the backdrop shows: the one on the first display
// that reports playback, else the first of the playlist or the library.
func (a *application) ambientID() string {
	for _, d := range a.displays {
		if s := a.displayState(d.Identifier); s.ok {
			return s.media.ID
		}
	}
	if len(a.config.Playlist.VideoIDs) > 0 {
		return a.config.Playlist.VideoIDs[0]
	}
	if len(a.config.Media) > 0 {
		return a.config.Media[0].ID
	}
	return ""
}

func (a *application) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(titleBarHeight).Shrink(0).DragWindow()
	ui.Column(c).Padding(0, 16, 14, 18).Gap(2).Children(func() {
		ui.Text(c, "Loopwall").FontSize(17).Bold()
		ui.Text(c, "本地视频壁纸").FontSize(12).TextColor(t.TextMuted)
	})
	ui.Column(c).Grow(1).Children(func() {
		glassNav(c, &a.section, []navItem{
			{sectionLibrary, iconLibrary, "资料库", strconv.Itoa(len(a.config.Media))},
			{sectionPlay, iconPlaylist, "播放与显示器", strconv.Itoa(len(a.config.Playlist.VideoIDs))},
			{sectionNearby, iconNearby, "附近的 Mac", a.nearbyBadge()},
			{sectionSetting, iconSettings, "设置", ""},
		})
	})
	// Live state of each display, always in view.
	ui.Column(c).Padding(12, 14, 16, 14).Gap(10).Children(func() {
		ui.Text(c, "正在播放").FontSize(11).Bold().TextColor(t.TextMuted)
		if len(a.displays) == 0 {
			ui.Text(c, "未检测到显示器").FontSize(12).TextColor(t.TextMuted)
		}
		if a.wallpaperStalled(time.Now()) {
			glassButton(c, iconAlert, "修复壁纸", true).OnClick(func() { a.repairWallpaper("正在重建墙纸服务…", time.Now()) })
		}
		for _, d := range a.displays {
			s := a.displayState(d.Identifier)
			ui.Row(c.Key("side-" + d.Identifier)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Shrink(0).Background(s.color(t))
				ui.Column(c).Gap(1).MinWidth(0).Grow(1).Children(func() {
					ui.Text(c, d.Name).FontSize(12).FontWeight(600).SingleLine()
					ui.Text(c, s.short()).FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			})
		}
	})
}

// pageFrame lays out a page whose content scrolls under a floating header:
// the content blurs progressively as it reaches the top, and the header's
// title and glass buttons stay on it.
func pageFrame(c *ui.Context, title, subtitle string, actions func(), body func(top float32)) {
	bar := c.TitleBar()
	ui.Box(c).Fill().Children(func() {
		body(headerHeight)
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(headerHeight).PassThrough().
			Material(glass.Blur{Radius: 16, Mask: &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180, Start: 0.55, End: 1}})
		// The backdrop's tone over it, so what scrolls under the title fades
		// out rather than shows through it, as a scroll edge does.
		edge := current.edgeTone(c.Theme())
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(headerHeight).PassThrough().Gradient(edge, edge.Alpha(0), 180)
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(headerHeight).AlignItems(ui.Center).Gap(10).
			Padding(0, bar.Right+20, 0, 24).DragWindow().Children(func() {
			ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				current.onBackdrop(c, ui.Text(c, title).FontSize(20).Bold().SingleLine(), false)
				if subtitle != "" {
					current.onBackdrop(c, ui.Text(c, subtitle).FontSize(12).SingleLine(), true)
				}
			})
			if actions != nil {
				actions()
			}
		})
	})
}

// scrollBody is a page's scrolling column, starting under the header.
func scrollBody(c *ui.Context, top, maxWidth float32, fn func()) {
	overlay(ui.Scroll(c)).ScrollbarInsets(top, 0, 0, 0).Children(func() {
		if maxWidth == 0 {
			ui.Column(c).FillWidth().Padding(top+14, 24, 28, 24).Gap(26).Children(fn)
			return
		}
		// Centered in the pane, as System Settings centers its column.
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(top+14, 24, 28, 24).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(maxWidth).Gap(26).Children(fn)
		})
	})
}

func (a *application) libraryView(c *ui.Context) {
	inList := 0
	for _, m := range a.config.Media {
		if slices.Contains(a.config.Playlist.VideoIDs, m.ID) {
			inList++
		}
	}
	subtitle := fmt.Sprintf("%d 个视频 · %d 个在轮播中 · 原画质，静音播放", len(a.config.Media), inList)
	pageFrame(c, "资料库", subtitle, func() {
		label := "导入视频"
		if a.busy {
			label = "处理中…"
		}
		glassButton(c, iconImport, label, true).Disabled(a.busy).OnClick(a.chooseImport)
	}, func(top float32) {
		if len(a.config.Media) == 0 {
			emptyState(c, iconFilm, "还没有视频", "把视频或文件夹拖到这里，或点右上角「导入视频」。支持 MP4、MOV、M4V，原画质复制，不重新编码。")
			return
		}
		a.grid.Key = func(i int) any { return a.config.Media[i].ID }
		a.grid.Label = func(i int) string { return a.config.Media[i].Name }
		overlay(ui.GridView(c, &a.grid, len(a.config.Media), 220, 214, func(i int) {
			a.mediaCard(c, a.config.Media[i])
		})).ScrollbarInsets(top, 0, 0, 0).Gap(18).Padding(top+14, 24, 24, 24)
	})
}

func (a *application) mediaCard(c *ui.Context, m catalog.MediaEntry) {
	t := c.Theme()
	order := slices.Index(a.config.Playlist.VideoIDs, m.ID)
	ui.Column(c).Grow(1).Gap(8).Children(func() {
		// Thumbnail with the order in the playlist and the duration on it.
		thumb := ui.Box(c).Grow(1).MinHeight(0).Radius(10).Clip().Background(t.Surface).Cursor(ui.CursorPointer).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12)).Label("预览 " + m.Name)
		if thumb.Clicked() {
			a.previewMedia(m)
		}
		hovered := thumb.Hovered()
		thumb.Children(func() {
			if bm := a.thumbs[m.ID]; bm != nil {
				ui.Image(c, bm).Fill().Fit(ui.Cover)
			} else {
				ui.Box(c).Fill().Center().TextColor(t.TextMuted).Children(func() { ui.Icon(c, iconFilm).FontSize(28) })
			}
			if hovered {
				overlay(ui.Box(c)).PassThrough().Center().Children(func() {
					current.fillControl(c, ui.Box(c).Size(56, 56).Radius(28).Center(), mediaGlass).TextColor(ui.RGB(255, 255, 255)).Children(func() {
						ui.Icon(c, iconPlay).FontSize(20)
					})
				})
			}
			if order >= 0 {
				glassPill(c, fmt.Sprintf("#%d 轮播", order+1), glass.Glass{Tint: t.Accent}, t.AccentText).Absolute().Top(8).Left(8)
			}
			glassPill(c, clock(m.DurationSeconds), mediaGlass, ui.RGB(255, 255, 255)).Absolute().Bottom(8).Right(8)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if a.selected == m.ID {
				ui.TextInput(c, &a.rename).Grow(1).Radius(999).Label("视频名称").OnSubmit(func() { a.commitRename(m.ID) })
				iconButton(c, iconCheck, "保存名称").Disabled(a.busy || strings.TrimSpace(a.rename) == "").OnClick(func() { a.commitRename(m.ID) })
				iconButton(c, iconX, "取消").OnClick(func() { a.selected = "" })
				return
			}
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				current.onBackdrop(c, ui.Text(c, m.Name).FontSize(13).FontWeight(600).SingleLine(), false)
				meta := fmt.Sprintf("%s · %s fps", resolution(m.Width, m.Height), trimFloat(m.FrameRate))
				if on := a.pinnedOn(m.ID); len(on) > 0 {
					meta += " · 固定在 " + strings.Join(on, "、")
				}
				current.onBackdrop(c, ui.Text(c, meta).FontSize(11).SingleLine(), true)
			})
			if order >= 0 {
				iconButton(c, iconCheck, "移出轮播").TextColor(t.Accent).Disabled(a.busy).OnClick(func() { a.togglePlaylist(m.ID) })
			} else {
				iconButton(c, iconPlus, "加入轮播").Disabled(a.busy).OnClick(func() { a.togglePlaylist(m.ID) })
			}
			iconButton(c, iconMore, "更多").Disabled(a.busy).Menu(func(menu *ui.Menu) {
				if menu.Item("预览").Chosen() {
					a.previewMedia(m)
				}
				if menu.Item("重命名…").Chosen() {
					a.selected, a.rename = m.ID, m.Name
				}
				menu.Submenu("设为壁纸", func(sub *ui.Menu) {
					for _, d := range a.displays {
						on := a.config.Playlist.Pins[d.Identifier] == m.ID
						if sub.Item(d.Name).ID("wall-" + d.Identifier).Checked(on).Chosen() {
							if on {
								a.pinDisplay(d.Identifier, "") // chosen again: back to the playlist
							} else {
								a.pinDisplay(d.Identifier, m.ID)
							}
						}
					}
					sub.Separator()
					if sub.Item("所有显示器").Chosen() {
						a.pinAll(m.ID)
					}
				}).Disabled(len(a.displays) == 0)
				menu.Separator()
				if menu.Item("从资料库删除…").Chosen() {
					a.deleteMedia(m)
				}
			})
		})
	})
}

func (a *application) commitRename(id string) {
	name := strings.TrimSpace(a.rename)
	if a.busy || name == "" {
		return
	}
	for i := range a.config.Media {
		if a.config.Media[i].ID == id {
			a.config.Media[i].Name = name
		}
	}
	a.selected = ""
	a.save()
}

func (a *application) togglePlaylist(id string) {
	if slices.Contains(a.config.Playlist.VideoIDs, id) {
		a.config.Playlist.VideoIDs = slices.DeleteFunc(a.config.Playlist.VideoIDs, func(v string) bool { return v == id })
	} else {
		a.config.Playlist.VideoIDs = append(a.config.Playlist.VideoIDs, id)
	}
	a.save()
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
						if glassSegmented(c, &mode, "切换时机", a.busy, "播完一遍", "按时长") {
							a.config.Playlist.AdvanceMode = []string{"end", "duration"}[mode]
							a.save()
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
		thumbBox(c, a.thumbs[m.ID], 64, 36, 5)
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, m.Name).FontWeight(600).SingleLine()
			ui.Text(c, fmt.Sprintf("%s · %s", clock(m.DurationSeconds), resolution(m.Width, m.Height))).FontSize(11).TextColor(t.TextMuted)
		})
		ids := a.config.Playlist.VideoIDs
		controls := ui.Row(c).Gap(2).Opacity(0.55)
		if hovered {
			controls.Opacity(1)
		}
		controls.Children(func() {
			iconButton(c, iconUp, "上移").Disabled(a.busy || i == 0).OnClick(func() {
				ids[i-1], ids[i] = ids[i], ids[i-1]
				a.save()
			})
			iconButton(c, iconDown, "下移").Disabled(a.busy || i == n-1).OnClick(func() {
				ids[i+1], ids[i] = ids[i], ids[i+1]
				a.save()
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
			bm = a.thumbs[s.media.ID]
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
	if id == "" {
		delete(a.config.Playlist.Pins, key)
	} else {
		if a.config.Playlist.Pins == nil {
			a.config.Playlist.Pins = map[string]string{}
		}
		a.config.Playlist.Pins[key] = id
	}
	a.save()
}

// pinAll makes every display loop video id alone, saving once.
func (a *application) pinAll(id string) {
	if a.busy || len(a.displays) == 0 {
		return
	}
	if a.config.Playlist.Pins == nil {
		a.config.Playlist.Pins = map[string]string{}
	}
	for _, d := range a.displays {
		a.config.Playlist.Pins[d.Identifier] = id
	}
	a.save()
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
	a.config.Playlist.IntervalSeconds = n
	a.save()
}

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
						if glassSwitch(c, &a.config.Playlist.KeepAwakeWhenLockedOnAC, "接电锁屏保持亮屏", a.busy) {
							a.save()
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
			current.onBackdrop(c, ui.Text(c, strings.TrimSpace("Loopwall "+mygo.App.Version())+" · 基于 Phosphene v1.11.4 · MyGO v0.9.0 · MIT License").FontSize(11), true)
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

// --- display state ---

type displayState struct {
	ok       bool // a fresh report names a video for this display
	waiting  bool // reported but not shown by the system yet
	media    catalog.MediaEntry
	position float64
}

func (a *application) runtimeFresh() bool {
	return a.runtime.Fresh(time.Now())
}

func (a *application) displayState(key string) displayState {
	var s displayState
	if !a.runtimeFresh() {
		return s
	}
	for _, p := range a.runtime.Displays {
		if p.DisplayKey == key {
			s.media, s.ok = a.entry(p.VideoID)
			s.position = p.PositionSeconds
			s.waiting = !slices.Contains(a.runtime.ActiveDisplayKeys, key)
		}
	}
	return s
}

func (s displayState) color(t *ui.Theme) ui.Color {
	switch {
	case s.ok && !s.waiting:
		return t.Success
	case s.ok:
		return t.Warning
	}
	return t.TextMuted
}

func (s displayState) title() string {
	if s.ok {
		return s.media.Name
	}
	return "未启用本壁纸"
}

func (s displayState) short() string {
	switch {
	case s.ok && s.waiting:
		return "等待系统恢复展示"
	case s.ok:
		return s.media.Name
	}
	return "未启用"
}

func (s displayState) detail() string {
	if s.ok && s.waiting {
		return "等待系统恢复展示"
	}
	return "在系统墙纸设置中选择「Loopwall 轮播」"
}

// --- formatting ---

// shortPath writes the home directory as ~, as Finder's path bar does.
func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// shorten cuts s to n characters, ending it with an ellipsis.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func clock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func resolution(w, h int64) string {
	switch {
	case w >= 7680 || h >= 4320:
		return "8K"
	case w >= 3840 || h >= 2160:
		return "4K"
	case h >= 1440:
		return "1440p"
	case h >= 1080:
		return "1080p"
	}
	return fmt.Sprintf("%d×%d", w, h)
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
}
