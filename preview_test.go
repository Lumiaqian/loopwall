package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/ui"
)

// TestRedrawUntil checks that a preview window keeps redrawing until its
// first picture shows, and no longer; and gives up at its limit.
func TestRedrawUntil(t *testing.T) {
	done := make(chan struct{})
	var n atomic.Int32
	finished := make(chan struct{})
	go func() {
		redrawUntil(done, time.Millisecond, time.Minute, func() { n.Add(1) })
		close(finished)
	}()
	time.Sleep(30 * time.Millisecond)
	if n.Load() < 5 {
		t.Fatalf("redrew %d times in 30ms, every 1ms", n.Load())
	}
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("still redrawing after the picture showed")
	}
	after := n.Load()
	time.Sleep(10 * time.Millisecond)
	if n.Load() != after {
		t.Error("redrew after it returned")
	}

	start := time.Now()
	redrawUntil(make(chan struct{}), time.Millisecond, 20*time.Millisecond, func() {})
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("gave up after %v, not at its 20ms limit", d)
	}
}

type fakePreviewPlayer struct {
	ready     bool
	buffering bool
	playing   bool
	pos       time.Duration
	dur       time.Duration
	err       error
	seeks     []time.Duration
	closed    bool
}

func (f *fakePreviewPlayer) Ready() bool             { return f.ready }
func (f *fakePreviewPlayer) Buffering() bool         { return f.buffering }
func (f *fakePreviewPlayer) Playing() bool           { return f.playing }
func (f *fakePreviewPlayer) Play()                   { f.playing = true }
func (f *fakePreviewPlayer) Pause()                  { f.playing = false }
func (f *fakePreviewPlayer) Seek(d time.Duration)    { f.pos = d; f.seeks = append(f.seeks, d) }
func (f *fakePreviewPlayer) Position() time.Duration { return f.pos }
func (f *fakePreviewPlayer) Duration() time.Duration { return f.dur }
func (f *fakePreviewPlayer) Err() error              { return f.err }
func (f *fakePreviewPlayer) Close()                  { f.closed = true }

func TestPreviewNeighbor(t *testing.T) {
	a, b := media(idOne), media(idTwo)
	if _, _, _, ok := previewNeighbor(nil, idOne, 1); ok {
		t.Fatal("empty list reported neighbor")
	}
	if got, idx, total, ok := previewNeighbor([]catalog.MediaEntry{a}, idOne, 1); ok || idx != 0 || total != 1 || got.ID != idOne {
		t.Fatalf("single item: got (%s,%d,%d,%v)", got.ID, idx, total, ok)
	}
	got, idx, total, ok := previewNeighbor([]catalog.MediaEntry{a, b}, idOne, -1)
	if !ok || idx != 1 || total != 2 || got.ID != idTwo {
		t.Fatalf("wrap backward: got (%s,%d,%d,%v)", got.ID, idx, total, ok)
	}
	got, idx, total, ok = previewNeighbor([]catalog.MediaEntry{a, b}, idTwo, 1)
	if !ok || idx != 0 || total != 2 || got.ID != idOne {
		t.Fatalf("wrap forward: got (%s,%d,%d,%v)", got.ID, idx, total, ok)
	}
}

func TestPreviewSessionControlsAndStates(t *testing.T) {
	realOpen := openPreviewVideo
	t.Cleanup(func() { openPreviewVideo = realOpen })

	players := map[string]*fakePreviewPlayer{}
	openPreviewVideo = func(path string) (previewPlayer, *ui.VideoPlayer, error) {
		fp := &fakePreviewPlayer{ready: true, playing: true, dur: 20 * time.Second, pos: 5 * time.Second}
		players[path] = fp
		return fp, nil, nil
	}

	m1, m2 := media(idOne), media(idTwo)
	m1.Name, m2.Name = "第一段", "第二段"
	m1.DurationSeconds, m2.DurationSeconds = 20, 40
	app := &application{
		root:   t.TempDir(),
		config: catalog.Config{SchemaVersion: 1, Media: []catalog.MediaEntry{m1, m2}},
	}
	closed, title := false, ""
	sess := &previewSession{
		app:         app,
		closeWin:    func() { closed = true },
		setTitle:    func(s string) { title = s },
		renderVideo: func(*ui.Context) {},
	}
	sess.open(m1)
	if title != "第一段 · 预览" {
		t.Fatalf("initial title = %q", title)
	}

	tt := ui.NewTester(sess.view, 960, 580)
	// Hover window so HUD appears, then toggle play/pause via Space.
	tt.Move(480, 300)
	tt.Advance(time.Second)
	if _, ok := tt.Find("0:05 / 0:20"); !ok {
		t.Fatal("HUD did not display current time / duration")
	}
	tt.Key(0, ui.KeySpace)
	p1 := players[catalog.VideoPath(app.root, m1)]
	if p1.playing {
		t.Fatal("Space did not pause playback")
	}
	tt.Key(0, ui.KeySpace)
	if !p1.playing {
		t.Fatal("Space did not resume playback")
	}

	// Arrow Right switches to next video and closes the old player.
	tt.Key(0, ui.KeyRight)
	if !p1.closed || sess.media.ID != idTwo || title != "第二段 · 预览" {
		t.Fatalf("Right did not switch to next video: closed=%v id=%s title=%q", p1.closed, sess.media.ID, title)
	}
	// Arrow Left switches back.
	tt.Key(0, ui.KeyLeft)
	if sess.media.ID != idOne || title != "第一段 · 预览" {
		t.Fatalf("Left did not switch back: id=%s title=%q", sess.media.ID, title)
	}

	// Drag progress slider to seek on the current player.
	p1 = players[catalog.VideoPath(app.root, m1)]
	sliderRect, ok := tt.Find("播放进度")
	if !ok {
		t.Fatal("progress slider not found")
	}
	tt.Press(sliderRect.X+sliderRect.W/4, sliderRect.Y+sliderRect.H/2)
	tt.Move(sliderRect.X+sliderRect.W*3/4, sliderRect.Y+sliderRect.H/2)
	tt.Release(sliderRect.X+sliderRect.W*3/4, sliderRect.Y+sliderRect.H/2)
	if len(p1.seeks) == 0 || p1.pos <= 10*time.Second {
		t.Fatalf("dragging slider did not seek forward: seeks=%v pos=%v", p1.seeks, p1.pos)
	}

	// Next button also switches videos after scrubbing.
	if err := tt.Click("下一个"); err != nil {
		t.Fatal(err)
	}
	if !p1.closed || sess.media.ID != idTwo {
		t.Fatalf("Next button did not switch: closed=%v id=%s", p1.closed, sess.media.ID)
	}

	// Escape closes the preview window.
	tt.Key(0, ui.KeyEscape)
	if !closed {
		t.Fatal("Escape did not close preview window")
	}

	// Error and timeout states show friendly card and retry.
	var failOpen bool
	openPreviewVideo = func(string) (previewPlayer, *ui.VideoPlayer, error) {
		if failOpen {
			return nil, nil, errors.New("decode failed")
		}
		return &fakePreviewPlayer{ready: false, dur: 10 * time.Second}, nil, nil
	}
	failOpen = true
	sess.open(m1)
	tt2 := ui.NewTester(sess.view, 960, 580)
	if _, ok := tt2.Find("无法播放视频"); !ok {
		t.Fatal("open error did not display 无法播放视频")
	}

	// Timeout after previewFirstFrame when never becoming Ready.
	failOpen = false
	sess.open(m1)
	tt3 := ui.NewTester(sess.view, 960, 580)
	if _, ok := tt3.Find("正在加载视频…"); !ok {
		t.Fatal("loading spinner capsule not shown")
	}
	tt3.Advance(previewFirstFrame + 50*time.Millisecond)
	if _, ok := tt3.Find("视频加载超时"); !ok {
		t.Fatal("timeout state not shown after previewFirstFrame")
	}
	openPreviewVideo = func(string) (previewPlayer, *ui.VideoPlayer, error) {
		return &fakePreviewPlayer{ready: true, playing: true, dur: 10 * time.Second}, nil, nil
	}
	if err := tt3.Click("重试"); err != nil {
		t.Fatal(err)
	}
	tt3.Advance(200 * time.Millisecond)
	if _, ok := tt3.Find("视频加载超时"); ok {
		t.Fatal("retry did not clear timeout state")
	}
}

func TestPreviewKeyboardSeekWithoutHover(t *testing.T) {
	player := &fakePreviewPlayer{ready: true, playing: true, dur: time.Minute}
	s := previewSession{ctrl: player}
	tt := ui.NewTester(s.view, 960, 580)
	tt.Move(-10, -10)
	tt.Advance(time.Second)
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyRight)
	if len(player.seeks) == 0 || player.pos <= 0 {
		t.Fatalf("keyboard seek: %v", player.seeks)
	}
}
