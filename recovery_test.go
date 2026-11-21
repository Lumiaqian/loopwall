package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
)

func TestWallpaperRecovery(t *testing.T) {
	restarts, real := 0, restartAgent
	restartAgent = func() error { restarts++; return nil }
	t.Cleanup(func() { restartAgent = real })

	t0 := time.Unix(1_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	report := func(s int, active ...string) catalog.Runtime {
		return catalog.Runtime{UpdatedAtUnix: float64(at(s).Unix()), Displays: []catalog.DisplayProgress{{DisplayKey: "A"}, {DisplayKey: "B"}}, ActiveDisplayKeys: active}
	}
	a := &application{root: t.TempDir()}

	// Playing: nothing offered.
	a.observeWallpaper(report(0, "A", "B"), 0, at(0))
	if a.wallpaperStalled(at(0)) {
		t.Fatal("playing wallpaper offered a repair")
	}
	// Waiting: offered only once it lasts, never repaired by itself.
	a.observeWallpaper(report(1), 0, at(1))
	a.observeWallpaper(report(15), 0, at(15))
	if a.wallpaperStalled(at(15)) {
		t.Fatal("offered a repair before stallAfter")
	}
	a.observeWallpaper(report(25), 0, at(25))
	if !a.wallpaperStalled(at(25)) || restarts != 0 {
		t.Fatalf("waiting 24s: stalled=%v restarts=%d", a.wallpaperStalled(at(25)), restarts)
	}
	// Playing again clears it.
	a.observeWallpaper(report(26, "A"), 0, at(26))
	if a.wallpaperStalled(at(26)) {
		t.Fatal("still offered once a display shows")
	}
	// A silent extension counts too.
	a.observeWallpaper(report(26, "A", "B"), 0, at(60))
	a.observeWallpaper(report(26, "A", "B"), 0, at(90))
	if !a.wallpaperStalled(at(90)) {
		t.Fatal("silent extension not offered a repair")
	}
	// The user repairs: one restart, not offered again while it settles.
	a.repairWallpaper("", at(90))
	a.repairWallpaper("", at(95))
	a.observeWallpaper(report(26), 0, at(96))
	a.observeWallpaper(report(26), 0, at(118))
	if restarts != 1 || a.wallpaperStalled(at(118)) {
		t.Fatalf("after a repair: restarts=%d stalled=%v", restarts, a.wallpaperStalled(at(118)))
	}
	// The extension calls for help (stamping first): repaired at once, once per call.
	stamp(t, a.root, at(200))
	a.observeWallpaper(report(200, "A", "B"), 1, at(200))
	a.observeWallpaper(report(202, "A", "B"), 1, at(202))
	if restarts != 2 {
		t.Fatalf("call for help: restarts=%d, want 2", restarts)
	}
}

// Power saving holds every display on purpose; that is not a stall, but a
// silent extension under it still is.
func TestPowerSavingIsNotAStall(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	held := func(s int) catalog.Runtime {
		return catalog.Runtime{UpdatedAtUnix: float64(at(s).Unix()), Displays: []catalog.DisplayProgress{{DisplayKey: "A"}}, PowerSavingActive: true}
	}
	a := &application{root: t.TempDir()}
	for s := 0; s <= 60; s += 5 {
		a.observeWallpaper(held(s), 0, at(s))
	}
	if a.wallpaperStalled(at(60)) {
		t.Fatal("power saving offered a repair")
	}
	// Without the flag the same report is a stall.
	r := held(60)
	r.PowerSavingActive = false
	a.observeWallpaper(r, 0, at(60))
	a.observeWallpaper(r, 0, at(85))
	if !a.wallpaperStalled(at(85)) {
		t.Fatal("no active display without power saving must still be offered a repair")
	}
	// Power saving does not hide a silent extension.
	b := &application{root: t.TempDir()}
	b.observeWallpaper(held(0), 0, at(40))
	b.observeWallpaper(held(0), 0, at(70))
	if !b.wallpaperStalled(at(70)) {
		t.Fatal("silent extension under power saving not offered a repair")
	}
}

func stamp(t *testing.T, root string, at time.Time) {
	t.Helper()
	text := strconv.FormatFloat(float64(at.UnixNano())/1e9, 'f', -1, 64)
	if err := os.WriteFile(filepath.Join(root, recoveryStampName), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A stuck signal is a Darwin notification any process can post; only one
// with the extension's fresh stamp behind it repairs.
func TestStuckSignalNeedsFreshStamp(t *testing.T) {
	restarts, real := 0, restartAgent
	restartAgent = func() error { restarts++; return nil }
	t.Cleanup(func() { restartAgent = real })

	now := time.Unix(2_000_000, 0)
	cases := []struct {
		name  string
		stamp *time.Time
		want  int
	}{
		{"no stamp", nil, 0},
		{"stale stamp", ptr(now.Add(-11 * time.Second)), 0},
		{"stamp from the future", ptr(now.Add(2 * time.Second)), 0},
		{"fresh stamp", ptr(now.Add(-3 * time.Second)), 1},
		{"slightly ahead", ptr(now.Add(500 * time.Millisecond)), 1},
	}
	for _, c := range cases {
		restarts = 0
		a := &application{root: t.TempDir()}
		if c.stamp != nil {
			stamp(t, a.root, *c.stamp)
		}
		a.observeWallpaper(catalog.Runtime{}, 1, now)
		if restarts != c.want {
			t.Errorf("%s: restarts=%d, want %d", c.name, restarts, c.want)
		}
		if a.stuckSignals != 1 {
			t.Errorf("%s: signal not recorded", c.name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestRuntimeFreshAllowsHeartbeatGap(t *testing.T) {
	a := &application{}
	a.runtime.UpdatedAtUnix = float64(time.Now().Add(-(catalog.RuntimeHeartbeat + 8*time.Second)).Unix())
	if !a.runtimeFresh() {
		t.Fatal("a report one heartbeat plus a late write old counts as stale")
	}
	a.runtime.UpdatedAtUnix = float64(time.Now().Add(-(catalog.RuntimeStaleAfter + 10*time.Second)).Unix())
	if a.runtimeFresh() {
		t.Fatal("a report past the stale limit counts as fresh")
	}
}

// runtime.json keeps every display that ever appeared: only those
// connected now can be waiting.
func TestWaitingCountsConnectedDisplaysOnly(t *testing.T) {
	now := time.Unix(3_000_000, 0)
	r := catalog.Runtime{UpdatedAtUnix: float64(now.Unix()), Displays: []catalog.DisplayProgress{{DisplayKey: "gone"}, {DisplayKey: "here"}}}
	connected := []platform.DisplayInfo{{Identifier: "here"}}

	a := &application{root: t.TempDir(), displays: []platform.DisplayInfo{{Identifier: "other"}}}
	a.observeWallpaper(r, 0, now)
	if !a.stallSince.IsZero() {
		t.Fatal("a runtime of displays all gone counted as waiting")
	}
	a.displays = connected
	a.observeWallpaper(r, 0, now)
	if a.stallSince.IsZero() {
		t.Fatal("a connected display that shows nothing is not waiting")
	}
	a.stallSince = time.Time{}
	r.ActiveDisplayKeys = []string{"here"}
	a.observeWallpaper(r, 0, now)
	if !a.stallSince.IsZero() {
		t.Fatal("an active display counted as waiting")
	}
}
