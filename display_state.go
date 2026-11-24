package main

import (
	"math"
	"slices"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo/ui"
)

// --- display state ---

type displayState struct {
	ok       bool // a fresh report names a video for this display
	waiting  bool // reported but not shown by the system yet
	held     bool // paused on purpose by the power-saving setting
	media    catalog.MediaEntry
	position float64
}

func (a *application) runtimeFresh() bool {
	return a.runtime.Fresh(time.Now())
}

func (a *application) displayState(key string) displayState {
	return a.displayStateAt(key, time.Now())
}

// anyDisplayActive reports whether at least one connected display currently
// presents or pauses a Loopwall wallpaper.
func (a *application) anyDisplayActive() bool {
	now := time.Now()
	for _, d := range a.displays {
		if a.displayStateAt(d.Identifier, now).ok {
			return true
		}
	}
	return false
}

// playingOn names the displays currently actively presenting video id.
func (a *application) playingOn(id string) []string {
	now := time.Now()
	var names []string
	for _, d := range a.displays {
		s := a.displayStateAt(d.Identifier, now)
		if s.ok && !s.waiting && !s.held && s.media.ID == id {
			names = append(names, d.Name)
		}
	}
	return names
}

func (a *application) displayStateAt(key string, now time.Time) displayState {
	var s displayState
	if !a.runtime.Fresh(now) {
		return s
	}
	for _, p := range a.runtime.Displays {
		if p.DisplayKey == key {
			s.media, s.ok = a.entry(p.VideoID)
			inactive := !slices.Contains(a.runtime.ActiveDisplayKeys, key)
			s.held = inactive && a.runtime.PowerSavingActive
			s.waiting = inactive && !s.held
			active := s.ok && !s.held && !s.waiting
			s.position = extrapolatedPosition(p.PositionSeconds, s.media.DurationSeconds, a.runtime.UpdatedAtUnix, now, active)
		}
	}
	return s
}

// extrapolatedPosition advances basePos linearly from updatedAtUnix to now
// while a display is actively playing, wrapping around duration so the
// UI progress stays smooth between 10s extension heartbeats.
func extrapolatedPosition(basePos, duration, updatedAtUnix float64, now time.Time, active bool) float64 {
	if duration <= 0 {
		return max(0, basePos)
	}
	clamped := min(duration, max(0, basePos))
	if !active || updatedAtUnix <= 0 {
		return clamped
	}
	updatedAt := time.Unix(0, int64(updatedAtUnix*float64(time.Second)))
	elapsed := now.Sub(updatedAt).Seconds()
	if elapsed <= 0 {
		return clamped
	}
	elapsed = min(elapsed, catalog.RuntimeStaleAfter.Seconds())
	return math.Mod(clamped+elapsed, duration)
}

func (s displayState) color(t *ui.Theme) ui.Color {
	switch {
	case s.ok && s.held:
		return t.TextMuted
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
	case s.ok && s.held:
		return "省电模式已暂停"
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
