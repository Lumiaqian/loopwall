package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

// previewFirstFrame bounds how long a preview window keeps redrawing for
// its first picture: a video that takes longer has failed or is broken.
const previewFirstFrame = 15 * time.Second

// previewPlayer is the playback surface previewSession drives; *ui.VideoPlayer
// implements it, and tests supply a fake.
type previewPlayer interface {
	Ready() bool
	Buffering() bool
	Playing() bool
	Play()
	Pause()
	Seek(d time.Duration)
	Position() time.Duration
	Duration() time.Duration
	Err() error
	Close()
}

// openPreviewVideo opens path for preview playback; tests replace it.
var openPreviewVideo = func(path string) (previewPlayer, *ui.VideoPlayer, error) {
	p, err := video.Open(path)
	if err != nil {
		return nil, nil, err
	}
	p.SetLoop(true).SetMuted(true)
	return p, p, nil
}

type previewSession struct {
	app         *application
	media       catalog.MediaEntry
	player      *ui.VideoPlayer
	ctrl        previewPlayer
	thumb       *ui.Bitmap
	openedAt    time.Time
	openErr     string
	timedOut    bool
	scrubbing   bool
	sliderRef   ui.Handle
	stopRedraw  func()
	closeWin    func()
	setTitle    func(string)
	invalidate  func()
	renderVideo func(*ui.Context)
}

func (s *previewSession) stop() {
	if s.stopRedraw != nil {
		s.stopRedraw()
		s.stopRedraw = nil
	}
}

func (s *previewSession) open(m catalog.MediaEntry) {
	s.stop()
	if s.ctrl != nil {
		s.ctrl.Close()
		s.ctrl = nil
	}
	s.player = nil
	if s.app != nil {
		s.app.preview = nil
	}
	s.media = m
	s.openedAt = time.Time{}
	s.openErr = ""
	s.timedOut = false
	s.scrubbing = false
	if s.app != nil {
		s.thumb = s.app.thumb(m.ID)
	}
	if s.setTitle != nil {
		s.setTitle(m.Name + " · 预览")
	}
	ctrl, player, err := openPreviewVideo(catalog.VideoPath(s.appRoot(), m))
	if err != nil {
		s.openErr = err.Error()
		if s.invalidate != nil {
			s.invalidate()
		}
		return
	}
	s.ctrl = ctrl
	s.player = player
	if s.app != nil {
		s.app.preview = player
	}
	if s.invalidate != nil && player != nil {
		// The player takes its first picture only as the window draws, and
		// nothing has the window draw again once the system decoded it: the
		// picture would wait for the pointer to move over the window. So the
		// window redraws until the picture shows.
		shown := make(chan struct{})
		var once sync.Once
		s.stopRedraw = func() { once.Do(func() { close(shown) }) }
		inv := s.invalidate
		go func() {
			redrawUntil(shown, 16*time.Millisecond, previewFirstFrame, inv)
			inv()
		}()
	}
}

func (s *previewSession) appRoot() string {
	if s.app == nil {
		return ""
	}
	return s.app.root
}

func (s *previewSession) mediaList() []catalog.MediaEntry {
	if s.app == nil {
		return nil
	}
	return s.app.config.Media
}

func (s *previewSession) step(delta int) {
	next, _, _, ok := previewNeighbor(s.mediaList(), s.media.ID, delta)
	if !ok {
		return
	}
	s.open(next)
}

func (s *previewSession) togglePlay() {
	if s.ctrl == nil || !s.ctrl.Ready() || s.openErr != "" || s.ctrl.Err() != nil {
		return
	}
	if s.ctrl.Playing() {
		s.ctrl.Pause()
	} else {
		s.ctrl.Play()
	}
}

func (s *previewSession) close() {
	if s.closeWin != nil {
		s.closeWin()
	}
}

var previewStarter = (*application).startPreview

// previewMedia opens a window playing m, looped and muted. It shows the
// thumbnail at once, and the video from its first picture on.
func (a *application) previewMedia(m catalog.MediaEntry) {
	previewStarter(a, m)
}

func (a *application) startPreview(m catalog.MediaEntry) {
	if a.previewWindow != nil {
		a.previewWindow.Close()
	}
	sess := &previewSession{app: a}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:   m.Name + " · 预览",
		Width:   960,
		Height:  580,
		Content: ui.View(sess.view),
	})
	sess.closeWin = w.Close
	sess.setTitle = w.SetTitle
	sess.invalidate = w.Invalidate
	a.previewWindow = w
	sess.open(m)
	w.OnClosed(func() {
		sess.stop()
		if sess.ctrl != nil {
			sess.ctrl.Close()
			sess.ctrl = nil
		}
		sess.player = nil
		if a.previewWindow == w {
			a.previewWindow = nil
			a.preview = nil
		}
	})
}

func (s *previewSession) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		s.close()
		return
	}
	if c.Shortcut(0, ui.KeySpace) {
		s.togglePlay()
	}
	if !s.sliderRef.Focused(c) && c.Shortcut(0, ui.KeyLeft) {
		s.step(-1)
	}
	if !s.sliderRef.Focused(c) && c.Shortcut(0, ui.KeyRight) {
		s.step(1)
	}

	if s.openedAt.IsZero() {
		s.openedAt = c.Now()
	}
	var pErr error
	var ready, buffering, playing bool
	if s.ctrl != nil {
		pErr = s.ctrl.Err()
		ready = s.ctrl.Ready()
		buffering = s.ctrl.Buffering()
		playing = s.ctrl.Playing()
	}
	errMsg := s.openErr
	if errMsg == "" && pErr != nil {
		errMsg = pErr.Error()
	}
	hasErr := errMsg != ""
	if ready || hasErr {
		s.stop()
	} else if !s.timedOut {
		if rem := previewFirstFrame - c.Now().Sub(s.openedAt); rem <= 0 {
			s.timedOut = true
			s.stop()
		} else {
			c.After(rem)
		}
	}

	root := ui.Box(c).Fill().Background(ui.RGB(0, 0, 0))
	hovered := root.Hovered()
	var hudTarget float32
	if root.FocusWithin() || hovered || !ready || !playing || hasErr || s.timedOut || s.scrubbing {
		hudTarget = 1
	}
	hud := root.AnimateTo("preview-hud", hudTarget, ui.Smooth)
	if ready && playing && hud > 0.01 {
		c.After(100 * time.Millisecond)
	}

	root.Children(func() {
		if (!ready || s.player == nil) && s.thumb != nil {
			overlay(ui.Image(c, s.thumb)).Fit(ui.Contain)
		}
		if s.renderVideo != nil {
			s.renderVideo(c)
		} else if s.player != nil {
			overlay(ui.Video(c, s.player)).Fit(ui.Contain)
		}

		switch {
		case hasErr:
			s.statusCard(c, "无法播放视频", errMsg, true)
		case !ready && s.timedOut:
			s.statusCard(c, "视频加载超时", "文件可能损坏或编码不受支持", true)
		case !ready || buffering:
			overlay(ui.Box(c)).PassThrough().Center().Children(func() {
				current.fillControl(c, ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 16).Radius(999), mediaGlass).
					Children(func() {
						ui.Spinner(c).TextColor(ui.RGB(255, 255, 255)).Label("正在加载视频")
						ui.Text(c, "正在加载视频…").FontSize(12).FontWeight(600).TextColor(ui.RGB(255, 255, 255))
					})
			})
		}

		s.controlBar(c, hud, ready, playing)
	})
}

func (s *previewSession) statusCard(c *ui.Context, title, detail string, canRetry bool) {
	white := ui.RGB(255, 255, 255)
	overlay(ui.Box(c)).Center().Children(func() {
		current.fillControl(c, ui.Column(c).Gap(10).Padding(20, 24).Radius(16).MaxWidth(360).AlignItems(ui.Center), mediaGlass).
			Children(func() {
				ui.Icon(c, iconAlert).FontSize(26).TextColor(white)
				ui.Text(c, title).FontSize(15).Bold().TextColor(white)
				if detail != "" {
					ui.Text(c, detail).FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.8)).MaxLines(3)
				}
				if canRetry {
					b := ui.ButtonBase(c.Key("preview-retry")).Padding(7, 18).Radius(999).
						Background(ui.RGBA(255, 255, 255, 0.18)).TextColor(white).Label("重试")
					if b.Pressed() {
						b.Background(ui.RGBA(255, 255, 255, 0.36))
					} else if b.Hovered() {
						b.Background(ui.RGBA(255, 255, 255, 0.28))
					}
					b.OnClick(func() { s.open(s.media) })
					b.Children(func() {
						ui.Text(c, "重试").FontSize(12).FontWeight(600).SingleLine()
					})
				}
			})
	})
}

func (s *previewSession) controlBar(c *ui.Context, hud float32, ready, playing bool) {
	white := ui.RGB(255, 255, 255)
	muted := ui.RGBA(255, 255, 255, 0.75)

	totalSec := s.media.DurationSeconds
	if s.ctrl != nil && s.ctrl.Duration() > 0 {
		totalSec = s.ctrl.Duration().Seconds()
	}
	var posSec float64
	if s.ctrl != nil {
		posSec = s.ctrl.Position().Seconds()
	}
	if totalSec > 0 {
		posSec = min(totalSec, max(0, posSec))
	} else {
		posSec = 0
	}

	_, idx, total, canSwitch := previewNeighbor(s.mediaList(), s.media.ID, 0)
	meta := fmt.Sprintf("%s · %s fps", resolution(s.media.Width, s.media.Height), trimFloat(s.media.FrameRate))
	if total > 1 && idx >= 0 {
		meta = fmt.Sprintf("%s · %d / %d", meta, idx+1, total)
	}

	ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Padding(0, 16, 16, 16).Opacity(hud).Children(func() {
		bar := current.fillControl(c, ui.Column(c).Gap(8).Padding(12, 16).Radius(16).FillWidth(), mediaGlass)
		bar.Children(func() {
			sliderMax := totalSec
			if sliderMax <= 0 {
				sliderMax = 1
			}
			const pad = 7
			slider := ui.SliderBase(c, &posSec, 0, sliderMax).FillWidth().Height(20).Padding(0, pad).
				Ref(&s.sliderRef).Label("播放进度").Disabled(s.ctrl == nil || !ready || totalSec <= 0)
			if slider.Changed() && s.ctrl != nil {
				s.ctrl.Seek(time.Duration(posSec * float64(time.Second)))
			}
			s.scrubbing = slider.Pressed() || slider.Dragging()
			frac := float32(0)
			if totalSec > 0 {
				frac = float32(min(1, max(0, posSec/totalSec)))
			}
			slider.Draw(func(p *ui.Painter, r ui.Rect) {
				cy := r.Y + r.H/2
				trackW := max(0, r.W-2*pad)
				track := ui.Rect{X: r.X + pad, Y: cy - 2, W: trackW, H: 4}
				p.Fill(track, ui.RGBA(255, 255, 255, 0.25), 2)
				if frac > 0 {
					p.Fill(ui.Rect{X: track.X, Y: track.Y, W: trackW * frac, H: 4}, white, 2)
				}
				knobR := float32(6)
				if slider.Pressed() || slider.Hovered() {
					knobR = 7
				}
				kx := track.X + trackW*frac
				p.Fill(ui.Rect{X: kx - knobR, Y: cy - knobR, W: knobR * 2, H: knobR * 2}, white, knobR)
			})

			ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
				mediaButton(c, iconPrev, "上一个", !canSwitch, func() { s.step(-1) })
				playIcon, playLabel := iconPlay, "播放"
				if playing {
					playIcon, playLabel = iconPause, "暂停"
				}
				mediaButton(c, playIcon, playLabel, s.ctrl == nil || !ready, s.togglePlay)
				mediaButton(c, iconNext, "下一个", !canSwitch, func() { s.step(1) })
				ui.Text(c, fmt.Sprintf("%s / %s", clock(posSec), clock(totalSec))).
					FontSize(12).FontWeight(600).TextColor(white).Margin(0, 0, 0, 4)
				ui.Box(c).Grow(1)
				ui.Text(c, meta).FontSize(11).TextColor(muted).SingleLine()
			})
		})
	})
}

func mediaButton(c *ui.Context, svg *ui.SVG, label string, disabled bool, onClick func()) {
	b := ui.ButtonBase(c.Key("preview-"+label)).Size(30, 30).Radius(15).Center().
		TextColor(ui.RGB(255, 255, 255)).Label(label).Tooltip(label).Disabled(disabled)
	switch {
	case b.Pressed():
		b.Background(ui.RGBA(255, 255, 255, 0.24))
	case b.Hovered():
		b.Background(ui.RGBA(255, 255, 255, 0.16))
	}
	b.OnClick(onClick)
	b.Children(func() { ui.Icon(c, svg).FontSize(15) })
}

// previewNeighbor returns the media item delta steps away from currentID
// (wrapping at the ends of list), its index, the list length, and whether
// stepping moves to another item.
func previewNeighbor(list []catalog.MediaEntry, currentID string, delta int) (catalog.MediaEntry, int, int, bool) {
	n := len(list)
	if n == 0 {
		return catalog.MediaEntry{}, -1, 0, false
	}
	idx := -1
	for i, m := range list {
		if m.ID == currentID {
			idx = i
			break
		}
	}
	if delta == 0 {
		if idx < 0 {
			return list[0], 0, n, n > 1
		}
		return list[idx], idx, n, n > 1
	}
	if n == 1 {
		return list[0], 0, 1, false
	}
	if idx < 0 {
		idx = 0
	}
	next := ((idx+delta)%n + n) % n
	return list[next], next, n, true
}

// redrawUntil calls redraw every interval until done closes, or limit
// passes.
func redrawUntil(done <-chan struct{}, every, limit time.Duration, redraw func()) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		select {
		case <-done:
			return
		case <-deadline.C:
			return
		case <-tick.C:
			redraw()
		}
	}
}
