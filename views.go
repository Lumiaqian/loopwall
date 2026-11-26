package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
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
	switch {
	case c.Shortcut(ui.Cmd, ui.Key1):
		a.section = sectionLibrary
	case c.Shortcut(ui.Cmd, ui.Key2):
		a.section = sectionPlay
	case c.Shortcut(ui.Cmd, ui.Key3):
		a.section = sectionNearby
	case c.Shortcut(ui.Cmd, ui.Key4), c.Shortcut(ui.Cmd, ui.KeyComma):
		a.section = sectionSetting
	case c.Shortcut(ui.Cmd, ui.KeyO):
		a.chooseImport()
	case c.Shortcut(ui.Cmd, ui.KeyF):
		a.section = sectionLibrary
		if len(a.config.Media) > 0 {
			a.focus = focusSearch
		}
	}
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
		a.importPaths(files, nil)
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
	if bm := a.thumb(a.ambientID()); bm != nil {
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
		setBadge, setAccent := "", false
		if updateAvailable() != "" {
			setBadge, setAccent = "新版本", true
		}
		glassNav(c, &a.section, []navItem{
			{id: sectionLibrary, icon: iconLibrary, label: "资料库", badge: strconv.Itoa(len(a.config.Media))},
			{id: sectionPlay, icon: iconPlaylist, label: "播放与显示器", badge: strconv.Itoa(len(a.config.Playlist.VideoIDs))},
			{id: sectionNearby, icon: iconNearby, label: "附近的 Mac", badge: a.nearbyBadge()},
			{id: sectionSetting, icon: iconSettings, label: "设置", badge: setBadge, accentBadge: setAccent},
		})
	})
	// Live state of each display, always in view.
	ui.Column(c).Padding(12, 14, 16, 14).Gap(10).Children(func() {
		ui.Text(c, "正在播放").FontSize(11).Bold().TextColor(t.TextMuted)
		if len(a.displays) == 0 {
			ui.Text(c, "未检测到显示器").FontSize(12).TextColor(t.TextMuted)
		}
		if a.wallpaperStalled(time.Now()) {
			repair := ui.ButtonBase(c.Key("side-repair")).FillWidth().Padding(8, 10).Radius(10).Gap(8).AlignItems(ui.Center).
				TextColor(t.Warning).Label("修复壁纸")
			switch {
			case repair.Pressed():
				repair.Background(t.Warning.Alpha(0.30))
			case repair.Hovered():
				repair.Background(t.Warning.Alpha(0.24))
			default:
				repair.Background(t.Warning.Alpha(0.16))
			}
			repair.OnClick(func() { a.startWallpaperRepair() })
			repair.Children(func() {
				ui.Icon(c, iconAlert).FontSize(15)
				ui.Text(c, "修复壁纸").FontSize(12).FontWeight(600).SingleLine()
			})
		}
		for _, d := range a.displays {
			s := a.displayState(d.Identifier)
			row := ui.ButtonBase(c.Key("side-"+d.Identifier)).FillWidth().Padding(6, 8).Radius(10).Gap(8).AlignItems(ui.Center).
				Label(d.Name + "：" + s.short()).Tooltip("查看播放与显示器")
			switch {
			case row.Pressed():
				row.Background(t.SurfacePressed)
			case row.Hovered():
				row.Background(t.SurfaceHover)
			}
			row.OnClick(func() { a.section = sectionPlay })
			row.Children(func() {
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
