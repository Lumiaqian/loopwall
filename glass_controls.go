package main

import (
	"time"

	"github.com/Lumiaqian/mygo-ui-sdk/behaviors"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Liquid Glass controls: a switch and a segmented control whose knob or
// lens swells into clear glass while pressed, dragged or sliding, follows
// the pointer, and, let go, settles on the nearest place.

// dragThreshold is how far the pointer moves before a press is a drag.
const dragThreshold = 3

// dragFrame is how often a control pressed asks for a frame: a release
// that ends a drag is no click and asks for none, so without it the knob
// or lens would wait, half way, for whatever draws the window next.
const dragFrame = 16 * time.Millisecond

// lensGlass is the glass of a knob or lens swollen by l, from 0 at rest,
// tinted white by rest, to 1, clear.
func lensGlass(rest, l float32) glass.Glass {
	g := glass.Glass{Tint: ui.RGBA(255, 255, 255, rest*(1-l))}
	if l > 0.3 {
		g.Style = glass.Clear
	}
	return g
}

// lensRim is the hairline that lights a lens's edge as it swells.
func lensRim(l float32) ui.Color { return ui.RGBA(255, 255, 255, 0.45*l) }

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// glassSegmented is a segmented control in the Liquid Glass style: a faint
// capsule track and a lens of glass over the chosen segment. A press swells
// the lens into clear glass, which follows the pointer across the segments
// and, let go, settles on the nearest; a click chooses the segment under
// it, and Enter or Space the next one. The track is one button, so no
// segment takes the press from it. It reports whether *selected changed.
func glassSegmented(c *ui.Context, selected *int, label string, disabled bool, labels ...string) bool {
	t := c.Theme()
	const w, h, pad = 96, 30, 3
	n := len(labels)
	tint := float32(0.35)
	if t.Dark {
		tint = 0.14
	}
	res := behaviors.SegmentedTrack(c.Key("seg-"+label), selected, behaviors.SegmentedTrackOptions{
		Label:         label,
		Labels:        labels,
		SegmentWidth:  w,
		Padding:       pad,
		Disabled:      disabled,
		DragThreshold: dragThreshold,
		FrameInterval: dragFrame,
	})
	track := res.Track.Padding(pad).Radius(999).Background(t.Text.Alpha(0.06)).
		Label(label + "：" + labels[*selected])
	target := res.Target
	x := track.AnimateTo("lens", target, ui.Smooth)
	if res.Dragging {
		x = target // under the pointer, without lag
	}
	var swell float32
	if res.Dragging || absf(x-target) > 0.5 {
		swell = 1
	}
	l := track.AnimateTo("swell", swell, ui.Smooth)
	if !current.glass {
		l = 0 // a solid lens does not swell
	}
	track.Children(func() {
		lw, lh := w+l*12, h+l*10
		lensLeft, lensTop := pad+x+(w-lw)/2, pad+(h-lh)/2
		ui.Box(c).Size(float32(n*w), h)
		// Three layers, ordered by ZIndex: the labels as they are, the lens
		// frosting them, and over it, clipped to it, the labels magnified
		// about its middle by up to a fifth as it swells, as through a lens.
		for i, lb := range labels {
			ui.Box(c).Absolute().Top(pad).Left(pad+float32(i*w)).Size(w, h).Center().ZIndex(0).Children(func() {
				ui.Text(c, lb).FontWeight(600).TextColor(t.TextMuted)
			})
		}
		lens := ui.Box(c).Absolute().Top(lensTop).Left(lensLeft).Size(lw, lh).Radius(999).ZIndex(1)
		g := lensGlass(tint, l)
		g.Style = glass.Regular // frosted, so the labels under it don't show twice
		current.fillControl(c, lens, g).InsetShadow(0, 0, 0, 1, lensRim(l))
		mag := 1 + 0.22*l
		middle := pad + x + w/2
		ui.Box(c).Absolute().Top(lensTop).Left(lensLeft).Size(lw, lh).Radius(999).Clip().PassThrough().ZIndex(2).Children(func() {
			for i, lb := range labels {
				at := middle + (pad+float32(i*w)+w/2-middle)*mag
				bw, bh := w*mag, h*mag
				ui.Box(c).Absolute().Left(at-bw/2-lensLeft).Top(pad+h/2-bh/2-lensTop).Size(bw, bh).Center().Children(func() {
					ui.Text(c, lb).FontSize(t.FontSize * mag).FontWeight(600).TextColor(t.Text)
				})
			}
		})
	})
	return res.Changed
}

// glassSwitch is a switch in the Liquid Glass style: a capsule, the accent
// when on, and a white knob that, while pressed, dragged or sliding, swells
// into a clear lens of glass larger than the track, bending the color under
// it, then settles back. The knob follows the pointer; let go, it snaps to
// the nearer side. It reports whether *on changed.
func glassSwitch(c *ui.Context, on *bool, label string, disabled bool) bool {
	t := c.Theme()
	const w, h, knobW, knobH, lensW, lensH, pad = 50, 28, 30, 22, 42, 34, 3
	const lo, hi = float32(pad), float32(w - knobW - pad)
	res := behaviors.SwitchTrack(c, on, behaviors.SwitchTrackOptions{
		Label:         label,
		Min:           lo,
		Max:           hi,
		Disabled:      disabled,
		DragThreshold: dragThreshold,
		FrameInterval: dragFrame,
	})
	sw := res.Switch.Size(w, h).Radius(999)
	target := res.Target
	x := sw.AnimateTo("knob", target, ui.Smooth)
	if res.Active {
		x = target // under the pointer, without lag
	}
	// The track's color follows the knob, so the lens has it to bend.
	sw.Background(t.Text.Alpha(0.12).MixOklab(t.Accent, sw.AnimateTo("on", res.Progress, ui.Smooth)))
	var swell float32
	if res.Active || sw.Pressed() || absf(x-target) > 0.5 {
		swell = 1
	}
	l := sw.AnimateTo("lens", swell, ui.Smooth)
	if !current.glass {
		l = 0 // a solid knob does not swell
	}
	kw, kh := knobW+l*(lensW-knobW), knobH+l*(lensH-knobH)
	sw.Children(func() {
		knob := ui.Box(c).Absolute().Top((h-kh)/2).Left(x+(knobW-kw)/2).Size(kw, kh).Radius(999).PassThrough()
		if current.glass {
			knob.Material(lensGlass(0.85, l)).InsetShadow(0, 0, 0, 1, lensRim(l))
		} else {
			knob.Background(ui.RGB(255, 255, 255)) // a switch's knob is white in either theme
		}
		knob.Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.18*(1-l)))
	})
	return res.Changed
}

// navItem is a page of the sidebar's navigation.
type navItem struct {
	id          string
	icon        *ui.SVG
	label       string
	badge       string // a count or update tag after the label, if any
	accentBadge bool
}

// glassNav is the sidebar's navigation in the Liquid Glass style: a lens
// of glass tinted with the accent under the page chosen, which slides to
// the next one chosen, swelling into clear glass on the way, as a tab
// bar's does. Up and Down choose too. It reports whether *selected changed.
func glassNav(c *ui.Context, selected *string, items []navItem) bool {
	t := c.Theme()
	const rowH, gap, inset = 34, 2, 10
	from := *selected
	at := 0
	for i, it := range items {
		if it.id == *selected {
			at = i
		}
	}
	nav := behaviors.NavList(c.Key("nav"), &at, len(items), behaviors.NavListOptions{
		Label: "分区",
	})
	nav.Root.Gap(gap).Padding(0, inset)
	*selected = items[at].id
	target := float32(at * (rowH + gap))
	y := nav.Root.AnimateTo("lens", target, ui.Smooth)
	var swell float32
	if absf(y-target) > 0.5 {
		swell = 1
	}
	l := nav.Root.AnimateTo("swell", swell, ui.Smooth)
	if !current.glass {
		l = 0
	}
	nav.Root.Children(func() {
		lens := ui.Box(c).Absolute().Top(y - l*3).Left(inset - l*4).Right(inset - l*4).Height(rowH + l*6).Radius(12).PassThrough().ZIndex(0)
		g := glass.Glass{Tint: t.Accent.Alpha(0.24 * (1 - l))}
		if l > 0.3 {
			g.Style = glass.Clear
		}
		current.fillControl(c, lens, g).InsetShadow(0, 0, 0, 1, lensRim(l))
		for i, it := range items {
			on := i == at
			b := nav.Item(c.Key("nav-"+it.id), i, it.label).Height(rowH).Padding(0, 10).Gap(10).Radius(12).AlignItems(ui.Center).
				ZIndex(1) // over the lens, which is placed Absolute
			if b.Hovered() && !on {
				b.Background(t.SurfaceHover)
			}
			b.Children(func() {
				ui.Icon(c, it.icon).FontSize(16).TextColor(t.Accent)
				name := ui.Text(c, it.label).Grow(1).SingleLine()
				if on {
					name.FontWeight(600)
				}
				if it.badge != "" {
					if it.accentBadge {
						ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(6, 6).Radius(3).Background(t.Accent)
							ui.Text(c, it.badge).FontSize(11).FontWeight(600).TextColor(t.Accent)
						})
					} else {
						ui.Text(c, it.badge).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
					}
				}
			})
		}
	})
	*selected = items[at].id
	return *selected != from
}
