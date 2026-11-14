package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// look is how much Liquid Glass the window shows (prefs.Glass): what the
// backdrop shows of the wallpaper, and what fills panes and controls.
type look struct {
	key, label string
	about      string
	glass      bool        // false: a plain background and solid surfaces
	blur       float32     // how much the backdrop blurs the wallpaper
	tone, dark float32     // how far the backdrop fades toward the theme's background, light and dark
	pane       glass.Glass // the glass of the sidebar, the one large pane of glass
	// The content layer (groups, cards) is no glass, as Apple keeps Liquid
	// Glass to the controls and navigation floating over content: a
	// standard material, the backdrop blurred by contentBlur under the
	// theme's background at content.
	content, contentBlur float32
}

// looks are the choices of prefs.Glass, in the order the settings show them.
// The more glass, the more of the wallpaper the glass has to bend: its
// refraction shows only over detail.
var looks = []look{
	{key: "none", label: "无", about: "纯色背景和面板，最清晰、最省电。"},
	{key: "", label: "标准", about: "模糊的壁纸作底，侧栏和按钮是玻璃。", glass: true, blur: 32, tone: 0.38, dark: 0.38, pane: glass.Glass{Clarity: 0.3}, content: 0.5},
	{key: "max", label: "极致", about: "壁纸清晰可见，玻璃边缘的折射最明显。", glass: true, blur: 8, tone: 0.2, dark: 0.42, pane: glass.Glass{Clarity: 0.5}, content: 0.45, contentBlur: 24},
}

// current is the look of the frame being built; view sets it first. The
// app has one window, built on the main thread.
var current = &looks[1]

func lookFor(key string) *look {
	for i := range looks {
		if looks[i].key == key {
			return &looks[i]
		}
	}
	return &looks[1]
}

// Solid surfaces of the look without glass, as grouped settings draw them:
// panes on a gray background, controls raised on panes.
func solidBackground(t *ui.Theme) ui.Color { return pick(t, ui.RGB(242, 242, 247), ui.RGB(28, 28, 30)) }
func solidPane(t *ui.Theme) ui.Color       { return pick(t, ui.RGB(255, 255, 255), ui.RGB(44, 44, 46)) }
func solidRaised(t *ui.Theme) ui.Color     { return pick(t, ui.RGB(255, 255, 255), ui.RGB(72, 72, 76)) }

func pick(t *ui.Theme, light, dark ui.Color) ui.Color {
	if t.Dark {
		return dark
	}
	return light
}

// fillPane fills a large pane: the look's glass, or a solid card.
func (l *look) fillPane(c *ui.Context, e ui.Element) ui.Element {
	if l.glass {
		return e.Material(l.pane)
	}
	t := c.Theme()
	return e.Background(solidPane(t)).Border(1, t.Text.Alpha(0.06))
}

// fillContent fills a pane of the content layer, a group or a card: the
// look's standard material, or a solid card.
func (l *look) fillContent(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	if !l.glass {
		return l.fillPane(c, e)
	}
	return e.Material(contentMaterial{blur: l.contentBlur, tint: pick(t, ui.RGBA(255, 255, 255, l.content), ui.RGBA(28, 28, 30, l.content))}).
		Border(1, t.Text.Alpha(0.07))
}

// contentMaterial is a standard material: what is under it blurred, if at
// all, under a tint of the background; opaque with Reduce Transparency.
type contentMaterial struct {
	blur float32
	tint ui.Color
}

func (m contentMaterial) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	tint := m.tint
	if p.Preferences().ReduceTransparency {
		tint.A = 255
	} else if m.blur > 0 {
		glass.Blur{Radius: m.blur}.PaintMaterial(p, box, radii)
	}
	p.FillRadii(box, radii, tint)
}

// edgeTone is the color that content fades into under the page's title:
// the backdrop's tone, or without glass the plain background.
func (l *look) edgeTone(c *ui.Context) ui.Color {
	t := c.Theme()
	if !l.glass {
		return solidBackground(t)
	}
	return pick(t, ui.RGBA(255, 255, 255, min(1, 1.6*l.tone)), ui.RGBA(0, 0, 0, min(1, 1.3*l.dark)))
}

// mediaGlass is the glass of a control over a thumbnail: clear, as Apple
// sets glass over photos and video, dimmed so white text on it reads.
var mediaGlass = glass.Glass{Style: glass.Clear, Tint: ui.RGBA(0, 0, 0, 0.28)}

// fillControl fills a control with g, or without glass, with its tint or a
// raised surface.
func (l *look) fillControl(c *ui.Context, e ui.Element, g glass.Glass) ui.Element {
	switch {
	case l.glass:
		return e.Material(g)
	case g.Tint.A > 0 && !isWhite(g.Tint):
		return e.Background(g.Tint)
	}
	t := c.Theme()
	return e.Background(solidRaised(t)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
}

// onBackdrop styles text set straight on the backdrop, which glass leaves
// light in places and dark in others: a firmer color, and a soft halo of
// the theme's background that keeps it apart from the wallpaper. muted is
// for secondary text.
func (l *look) onBackdrop(c *ui.Context, e ui.Element, muted bool) ui.Element {
	t := c.Theme()
	switch {
	case !l.glass && muted:
		return e.TextColor(t.TextMuted)
	case !l.glass:
		return e
	case muted:
		e.TextColor(t.Text.Alpha(0.72))
	}
	return e.TextShadow(0, 0, 6, t.Background.Alpha(0.75))
}

func isWhite(c ui.Color) bool { return c.R == 255 && c.G == 255 && c.B == 255 }
