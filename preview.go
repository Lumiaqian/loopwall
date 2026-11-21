package main

import (
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

// previewMedia opens a window playing m, looped and muted. It shows the
// thumbnail at once, and the video from its first picture on.
func (a *application) previewMedia(m catalog.MediaEntry) {
	if a.previewWindow != nil {
		a.previewWindow.Close()
	}
	p, err := video.Open(catalog.VideoPath(a.root, m))
	if err != nil {
		a.status = err.Error()
		return
	}
	p.SetLoop(true).SetMuted(true)
	a.preview = p
	thumb := a.thumb(m.ID)
	// The player takes its first picture only as the window draws, and
	// nothing has the window draw again once the system decoded it: the
	// picture would wait for the pointer to move over the window. So the
	// window redraws until the picture shows.
	shown := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(shown) }) }
	w := mygo.NewWindow(mygo.WindowOptions{Title: m.Name + " · 预览", Width: 960, Height: 580, Content: ui.View(func(c *ui.Context) {
		ready := p.Ready()
		if ready {
			stop()
		}
		ui.Box(c).Fill().Background(ui.RGB(0, 0, 0)).Children(func() {
			if !ready && thumb != nil {
				overlay(ui.Image(c, thumb)).Fit(ui.Contain)
			}
			overlay(ui.Video(c, p)).Fit(ui.Contain)
		})
	})})
	a.previewWindow = w
	go redrawUntil(shown, 16*time.Millisecond, previewFirstFrame, w.Invalidate)
	w.OnClosed(func() {
		stop()
		p.Close()
		if a.previewWindow == w {
			a.previewWindow = nil
			a.preview = nil
		}
	})
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
