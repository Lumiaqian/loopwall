package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

const (
	libraryFilterAll = iota
	libraryFilterInPlaylist
	libraryFilterNotInPlaylist
)

func filterLibraryMedia(media []catalog.MediaEntry, playlistIDs []string, query string, filter int) []catalog.MediaEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" && filter == libraryFilterAll {
		return media
	}
	var out []catalog.MediaEntry
	for _, m := range media {
		inList := slices.Contains(playlistIDs, m.ID)
		switch filter {
		case libraryFilterInPlaylist:
			if !inList {
				continue
			}
		case libraryFilterNotInPlaylist:
			if inList {
				continue
			}
		}
		if q != "" {
			nameMatch := strings.Contains(strings.ToLower(m.Name), q)
			resMatch := strings.Contains(strings.ToLower(resolution(m.Width, m.Height)), q)
			if !nameMatch && !resMatch {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

func (a *application) libraryView(c *ui.Context) {
	inList := 0
	var totalBytes int64
	for _, m := range a.config.Media {
		if slices.Contains(a.config.Playlist.VideoIDs, m.ID) {
			inList++
		}
		totalBytes += a.mediaSize(m)
	}
	subtitle := fmt.Sprintf("%d 个视频 · %d 个在轮播中 · 原画质，静音播放", len(a.config.Media), inList)
	if totalBytes > 0 {
		subtitle = fmt.Sprintf("%d 个视频 · %d 个在轮播中 · 共 %s · 原画质，静音播放", len(a.config.Media), inList, sizeText(totalBytes))
	}
	importLabel := "导入视频"
	if a.importing {
		importLabel = "正在查找视频…"
		if a.importTotal > 0 {
			importLabel = "导入中…"
		}
	}
	importAction := func(c *ui.Context) {
		glassButton(c, iconImport, importLabel, true).Disabled(a.choosingImport || a.importing).OnClick(a.chooseImport)
	}
	pageFrame(c, "资料库", subtitle, func() {
		if len(a.config.Media) == 0 {
			return
		}
		importAction(c)
	}, func(top float32) {
		showImport := a.importing && a.importTotal > 0
		showGuide := len(a.config.Media) > 0 && len(a.displays) > 0 && !a.anyDisplayActive()
		filtered := filterLibraryMedia(a.config.Media, a.config.Playlist.VideoIDs, a.libraryQuery, a.libraryFilter)
		if len(a.config.Media) == 0 {
			empty := func() {
				emptyState(c, iconFilm, "还没有视频", "把视频或文件夹拖到这里，或点下方按钮导入。支持 MP4、MOV、M4V，原画质复制，不重新编码。", func() {
					importAction(c)
				})
			}
			if showImport {
				ui.Column(c).Fill().Children(func() {
					ui.Box(c).Padding(top+10, 24, 6, 24).FillWidth().Children(func() { a.importProgressBanner(c) })
					empty()
				})
				return
			}
			empty()
			return
		}

		ui.Column(c).Fill().Children(func() {
			ui.Column(c).Padding(top+10, 24, 6, 24).Gap(8).FillWidth().Children(func() {
				if showImport {
					a.importProgressBanner(c)
				}
				if showGuide {
					a.wallpaperEnableBanner(c)
				}
				a.libraryToolbar(c)
			})
			if len(filtered) == 0 {
				emptyState(c, iconSearch, "没有匹配的视频", "换个关键词或切换筛选条件。", func() {
					glassButton(c, nil, "重置筛选", false).OnClick(func() {
						a.libraryQuery = ""
						a.libraryFilter = libraryFilterAll
					})
				})
				return
			}
			a.grid.Key = func(i int) any { return filtered[i].ID }
			a.grid.Label = func(i int) string { return filtered[i].Name }
			ui.GridView(c, &a.grid, len(filtered), 220, 214, func(i int) {
				a.mediaCard(c, filtered[i])
			}).Gap(18).Grow(1).MinHeight(0).FillWidth().Padding(8, 24, 24, 24)
		})
	})
}

func (a *application) libraryToolbar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).FillWidth().Children(func() {
		current.fillContent(c, ui.Row(c).Grow(1).MinWidth(140).Height(34).Padding(0, 12).Gap(8).AlignItems(ui.Center).Radius(999)).
			Children(func() {
				ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
				in := ui.TextInputBase(c, &a.libraryQuery).Search().Grow(1).MinWidth(0).
					Placeholder("搜索视频…").Label("搜索资料库").FocusBind(&a.focus, focusSearch)
				if in.Shortcut(0, ui.KeyEscape) && a.libraryQuery != "" {
					a.libraryQuery = ""
				}
				if a.libraryQuery != "" {
					iconButton(c, iconX, "清除搜索").OnClick(func() { a.libraryQuery = "" })
				}
			})
		glassSegmented(c, &a.libraryFilter, "筛选资料库", false, "全部", "轮播中", "未轮播")
	})
}

func (a *application) wallpaperEnableBanner(c *ui.Context) {
	t := c.Theme()
	current.fillContent(c, ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(10, 14).Radius(14).FillWidth()).Children(func() {
		ui.Icon(c, iconMonitor).FontSize(16).TextColor(t.Accent)
		ui.Text(c, "尚未在显示器启用「Loopwall 轮播」，前往系统墙纸设置选择即可开始播放").FontSize(12).Grow(1).MinWidth(0)
		glassButton(c, iconExternal, "系统墙纸设置", false).OnClick(openWallpaperSettings)
	})
}

func (a *application) importProgressBanner(c *ui.Context) {
	t := c.Theme()
	n := min(a.importDone+1, a.importTotal)
	frac := float64(a.importDone) / float64(a.importTotal)
	current.fillContent(c, ui.Column(c).Gap(8).Padding(12, 14).Radius(14).FillWidth()).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
			ui.Spinner(c).Label("正在导入")
			ui.Text(c, fmt.Sprintf("正在导入第 %d / %d 个…", n, a.importTotal)).FontWeight(600)
			if a.importCurrent != "" {
				ui.Text(c, a.importCurrent).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			}
		})
		ui.Progress(c, frac).FillWidth().Label("导入进度")
	})
}

func (a *application) mediaCard(c *ui.Context, m catalog.MediaEntry) {
	t := c.Theme()
	order := slices.Index(a.config.Playlist.VideoIDs, m.ID)
	inPlaylist := order >= 0
	card := ui.Column(c).Grow(1).Gap(8).ContextMenu(func(menu *ui.Menu) {
		a.mediaMenu(menu, m, inPlaylist)
	})
	card.Children(func() {
		// Thumbnail with playlist order, live playing badge, and duration.
		thumb := ui.ButtonBase(c.Key("preview-"+m.ID)).Grow(1).MinHeight(0).Radius(10).Clip().Background(t.Surface).Cursor(ui.CursorPointer).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12)).Label("预览 " + m.Name)
		if thumb.Clicked() {
			a.previewMedia(m)
		}
		var playTarget float32
		if thumb.Hovered() {
			playTarget = 1
		}
		playK := thumb.AnimateTo("hover-play", playTarget, ui.Smooth)
		thumb.Children(func() {
			if bm := a.thumb(m.ID); bm != nil {
				ui.Image(c, bm).Fill().Fit(ui.Cover)
			} else {
				ui.Box(c).Fill().Center().TextColor(t.TextMuted).Children(func() { ui.Icon(c, iconFilm).FontSize(28) })
			}
			if playK > 0.01 {
				scale := 0.86 + 0.14*playK
				overlay(ui.Box(c)).PassThrough().Center().Opacity(playK).Scale(scale, scale).Children(func() {
					current.fillControl(c, ui.Box(c).Size(56, 56).Radius(28).Center(), mediaGlass).TextColor(ui.RGB(255, 255, 255)).Children(func() {
						ui.Icon(c, iconPlay).FontSize(20)
					})
				})
			}
			if inPlaylist {
				glassPill(c, fmt.Sprintf("#%d 轮播", order+1), glass.Glass{Tint: t.Accent}, t.AccentText).Absolute().Top(8).Left(8)
			}
			if playing := a.playingOn(m.ID); len(playing) > 0 {
				current.fillControl(c, ui.Row(c).PassThrough().Gap(5).AlignItems(ui.Center).Padding(3, 9).Radius(999), mediaGlass).
					Absolute().Bottom(8).Left(8).Tooltip("正在播放：" + strings.Join(playing, "、")).
					Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(t.Success)
						ui.Text(c, "播放中").FontSize(11).FontWeight(600).TextColor(ui.RGB(255, 255, 255))
					})
			}
			glassPill(c, clock(m.DurationSeconds), mediaGlass, ui.RGB(255, 255, 255)).Absolute().Bottom(8).Right(8)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if a.selected == m.ID {
				in := ui.TextInput(c, &a.rename).Grow(1).Radius(999).Label("视频名称").
					Bind(&a.renameHandle).FocusBind(&a.focus, focusRename).OnSubmit(func() { a.commitRename(m.ID) })
				if in.Shortcut(0, ui.KeyEscape) {
					a.cancelRename()
				}
				iconButton(c, iconCheck, "保存名称").Disabled(strings.TrimSpace(a.rename) == "").OnClick(func() { a.commitRename(m.ID) })
				iconButton(c, iconX, "取消").OnClick(a.cancelRename)
				return
			}
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				nameEl := current.onBackdrop(c, ui.Text(c, m.Name).FontSize(13).FontWeight(600).SingleLine(), false).Tooltip("双击重命名")
				if nameEl.DoubleClicked() {
					a.startRename(m)
				}
				meta := fmt.Sprintf("%s · %s fps", resolution(m.Width, m.Height), trimFloat(m.FrameRate))
				if sz := a.mediaSize(m); sz > 0 {
					meta += " · " + sizeText(sz)
				}
				if on := a.pinnedOn(m.ID); len(on) > 0 {
					meta += " · 固定在 " + strings.Join(on, "、")
				}
				current.onBackdrop(c, ui.Text(c, meta).FontSize(11).SingleLine(), true)
			})
			if inPlaylist {
				iconButton(c, iconCheck, "移出轮播").TextColor(t.Accent).OnClick(func() { a.togglePlaylist(m.ID) })
			} else {
				iconButton(c, iconPlus, "加入轮播").OnClick(func() { a.togglePlaylist(m.ID) })
			}
			iconButton(c, iconMore, "更多").Menu(func(menu *ui.Menu) {
				a.mediaMenu(menu, m, inPlaylist)
			})
		})
	})
}

func (a *application) mediaMenu(menu *ui.Menu, m catalog.MediaEntry, inPlaylist bool) {
	if menu.Item("预览").Chosen() {
		a.previewMedia(m)
	}
	if menu.Item("重命名…").Chosen() {
		a.startRename(m)
	}
	toggleLabel := "加入轮播"
	if inPlaylist {
		toggleLabel = "移出轮播"
	}
	if menu.Item(toggleLabel).Chosen() {
		a.togglePlaylist(m.ID)
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
	if menu.Item("从资料库删除…").Danger().Chosen() {
		a.deleteMedia(m)
	}
}

func (a *application) startRename(m catalog.MediaEntry) {
	a.selected = m.ID
	a.rename = m.Name
	a.focus = focusRename
	a.renameHandle.SelectAll()
}

func (a *application) cancelRename() {
	a.selected = ""
	a.rename = ""
	if a.focus == focusRename {
		a.focus = focusNone
	}
}

func (a *application) commitRename(id string) {
	name := strings.TrimSpace(a.rename)
	if name == "" {
		return
	}
	a.cancelRename()
	a.commit(func(c *catalog.Config) {
		for i := range c.Media {
			if c.Media[i].ID == id {
				c.Media[i].Name = name
			}
		}
	})
}

func (a *application) togglePlaylist(id string) {
	a.commit(func(c *catalog.Config) {
		if slices.Contains(c.Playlist.VideoIDs, id) {
			c.Playlist.VideoIDs = slices.DeleteFunc(c.Playlist.VideoIDs, func(v string) bool { return v == id })
		} else {
			c.Playlist.VideoIDs = append(c.Playlist.VideoIDs, id)
		}
	})
}
