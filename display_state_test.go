package main

import (
	"math"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestExtrapolatedPosition(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	updated := float64(t0.Unix())

	// Active playback advances linearly and wraps around duration.
	got := extrapolatedPosition(5, 20, updated, t0.Add(6*time.Second), true)
	if math.Abs(got-11) > 1e-6 {
		t.Fatalf("active 5+6s = %v, want 11", got)
	}
	got = extrapolatedPosition(18, 20, updated, t0.Add(5*time.Second), true)
	if math.Abs(got-3) > 1e-6 {
		t.Fatalf("wrapped 18+5s mod 20 = %v, want 3", got)
	}

	// Held (power saving) or waiting does not advance.
	if g := extrapolatedPosition(7, 20, updated, t0.Add(5*time.Second), false); g != 7 {
		t.Fatalf("inactive advanced to %v, want 7", g)
	}
	// Clock skew backward does not go negative or move.
	if g := extrapolatedPosition(7, 20, updated, t0.Add(-5*time.Second), true); g != 7 {
		t.Fatalf("backward clock = %v, want 7", g)
	}
	// Caps at RuntimeStaleAfter.
	staleCap := math.Mod(2+catalog.RuntimeStaleAfter.Seconds(), 50)
	if g := extrapolatedPosition(2, 50, updated, t0.Add(2*time.Minute), true); math.Abs(g-staleCap) > 1e-6 {
		t.Fatalf("stale cap = %v, want %v", g, staleCap)
	}
}

func TestDisplayStateAtHeldAndActive(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	m := media(idOne)
	m.DurationSeconds = 30
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m}
	a := &application{
		config: cfg,
		runtime: catalog.Runtime{
			SchemaVersion:     1,
			UpdatedAtUnix:     float64(now.Add(-4 * time.Second).Unix()),
			Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: idOne, PositionSeconds: 10}, {DisplayKey: "B", VideoID: idOne, PositionSeconds: 10}},
			ActiveDisplayKeys: []string{"A"},
		},
	}
	if s := a.displayStateAt("A", now); !s.ok || s.held || s.waiting || math.Abs(s.position-14) > 1e-6 {
		t.Fatalf("active display A = %+v", s)
	}
	if s := a.displayStateAt("B", now); !s.ok || !s.waiting || s.position != 10 {
		t.Fatalf("waiting display B = %+v", s)
	}
	a.runtime.ActiveDisplayKeys = nil
	a.runtime.PowerSavingActive = true
	if s := a.displayStateAt("A", now); !s.ok || !s.held || s.position != 10 {
		t.Fatalf("held display A = %+v", s)
	}
}

func TestIntervalPresetsApply(t *testing.T) {
	cfg := catalog.Default()
	cfg.Playlist.AdvanceMode = "duration"
	cfg.Playlist.IntervalSeconds = 60
	a := &application{
		root:     t.TempDir(),
		host:     &fakeHost{},
		config:   cfg,
		interval: "60",
		section:  sectionPlay,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建"}},
	}
	tt := ui.NewTester(a.view, 960, 720)
	if err := tt.Click("5 分钟"); err != nil {
		t.Fatal(err)
	}
	if a.config.Playlist.IntervalSeconds != 300 || a.interval != "300" {
		t.Fatalf("preset did not apply 300s: interval=%q config=%d", a.interval, a.config.Playlist.IntervalSeconds)
	}
}
