package main

import (
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
	var s displayState
	if !a.runtimeFresh() {
		return s
	}
	for _, p := range a.runtime.Displays {
		if p.DisplayKey == key {
			s.media, s.ok = a.entry(p.VideoID)
			s.position = p.PositionSeconds
			inactive := !slices.Contains(a.runtime.ActiveDisplayKeys, key)
			s.held = inactive && a.runtime.PowerSavingActive
			s.waiting = inactive && !s.held
		}
	}
	return s
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
