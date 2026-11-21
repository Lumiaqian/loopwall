package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

// WallpaperAgent sometimes stops serving the extension: after the extension
// restarts it never asks it for surfaces again, or it spins on connections
// that call nothing. The sandboxed extension cannot recover from either; a
// restart of WallpaperAgent, which the app can make, rebuilds every
// display's wallpaper.

const (
	// stallAfter is how long the displays wait for the system, or the
	// extension stays silent, before the app offers a repair. Waiting is
	// also what a locked screen looks like, so the app only offers it.
	stallAfter = 20 * time.Second
	// silentAfter is how old the extension's last report is when it counts
	// as silent.
	silentAfter = 30 * time.Second
	// repairSettle is how long a repair takes to show, during which the app
	// neither offers nor makes another.
	repairSettle = 30 * time.Second
	// stampMaxAge and stampSkew bound how far the extension's recovery stamp
	// may lie behind or ahead of now for a stuck signal to be trusted.
	stampMaxAge = 10 * time.Second
	stampSkew   = time.Second
)

// recoveryStampName is the file the extension writes, as Unix seconds,
// immediately before it posts the stuck notification.
const recoveryStampName = "loopwall-last-recovery"

// recentRecoveryStamp reports whether the extension stamped a recovery
// just now. The Darwin notification carries no sender, so any local process
// could post it; only the extension, in its container, can write the stamp.
func recentRecoveryStamp(root string, now time.Time) bool {
	b, err := os.ReadFile(filepath.Join(root, recoveryStampName))
	if err != nil {
		return false
	}
	sec, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil {
		return false
	}
	age := now.Sub(time.Unix(0, int64(sec*float64(time.Second))))
	return age >= -stampSkew && age <= stampMaxAge
}

// restartAgent restarts WallpaperAgent, which launchd relaunches; tests
// replace it.
var restartAgent = func() error { return exec.Command("/usr/bin/killall", "-TERM", "WallpaperAgent").Run() }

// observeWallpaper follows the extension's report and its calls for help,
// on the main thread: a call for help repairs at once, a stall is offered.
func (a *application) observeWallpaper(r catalog.Runtime, stuckSignals int64, now time.Time) {
	if stuckSignals > a.stuckSignals {
		a.stuckSignals = stuckSignals
		// Ported from upstream Phosphene c6e8118: act only with the
		// extension's own fresh stamp.
		if recentRecoveryStamp(a.root, now) {
			a.repairWallpaper("壁纸扩展报告墙纸服务卡住，已自动重建", now)
		} else {
			log.Printf("recovery: ignoring a stuck signal without a fresh extension stamp")
		}
	}
	silent := r.UpdatedAtUnix > 0 && now.Sub(time.Unix(int64(r.UpdatedAtUnix), 0)) > silentAfter
	waiting := len(r.Displays) > 0 && len(r.ActiveDisplayKeys) == 0
	switch {
	case !silent && !waiting:
		a.stallSince = time.Time{}
	case a.stallSince.IsZero():
		a.stallSince = now
	}
}

// wallpaperStalled reports that the wallpaper has waited or been silent long
// enough for a repair to be worth offering.
func (a *application) wallpaperStalled(now time.Time) bool {
	return !a.stallSince.IsZero() && now.Sub(a.stallSince) > stallAfter && now.Sub(a.repairedAt) > repairSettle
}

// repairWallpaper restarts WallpaperAgent, at most once while a repair
// settles, and says so with message.
func (a *application) repairWallpaper(message string, now time.Time) {
	if now.Sub(a.repairedAt) < repairSettle {
		return
	}
	a.repairedAt, a.stallSince = now, time.Time{}
	if err := restartAgent(); err != nil {
		a.status = "重建墙纸服务失败：" + err.Error()
		return
	}
	a.status = message
}
