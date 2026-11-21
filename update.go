package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// Updates come from MyGO's updater (plugins/updater), which asks before it
// installs one, then replaces the whole bundle once the app exits and
// relaunches it. A bundle replaced so can lose its extension's PlugInKit
// registration, and WallpaperAgent goes on running the old extension until
// it restarts: so the first launch of a new version registers the extension
// again and restarts WallpaperAgent, as scripts/install-local.sh does.

// pluginkit runs /usr/bin/pluginkit; tests replace it.
var pluginkit = func(args ...string) ([]byte, error) {
	return exec.Command("/usr/bin/pluginkit", args...).CombinedOutput()
}

// launchedVersion records the version the app launches as, and reports
// whether an update replaced the app since it last ran: a version it did
// not run as last, after one it did. The first launch to record one, as
// after installing by hand, is no update.
func (a *application) launchedVersion(current string) bool {
	last := a.prefs.LastVersion
	if current == "" || current == last {
		return false
	}
	a.prefs.LastVersion = current
	if err := writePrefs(a.prefs); err != nil {
		a.status = "设置保存失败：" + err.Error()
	}
	return last != ""
}

// reloadExtension registers the extension in appex unless PlugInKit already
// has it registered at that path, off the main thread. A registration at
// another path, as an older copy of the app leaves, does not count: the
// system would keep loading that copy.
func reloadExtension(appex string) error {
	if out, err := pluginkit("-m", "-v", "-i", catalog.ExtensionID); err == nil && registeredAt(out, appex) {
		return nil
	}
	_, err := pluginkit("-a", appex)
	return err
}

// registeredAt reports whether the paths in the output of pluginkit -m -v
// include appex. Each plug-in line ends in its path after a tab.
func registeredAt(out []byte, appex string) bool {
	want := canonicalPath(appex)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 2 {
			continue
		}
		if path := strings.TrimSpace(fields[len(fields)-1]); filepath.IsAbs(path) && canonicalPath(path) == want {
			return true
		}
	}
	return false
}

// canonicalPath cleans path and resolves its symbolic links when it can.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// appBundle is the .app bundle the app runs from.
func appBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(exe))), nil // .app/Contents/MacOS/exe
}

// extensionBundle is the wallpaper extension in the bundle the app runs from.
func extensionBundle() (string, error) {
	app, err := appBundle()
	return filepath.Join(app, "Contents", "Extensions", "LoopwallExtension.appex"), err
}

// checkExtension registers the extension at each launch unless it is, as a
// replaced bundle or a hand install can leave it unregistered, and after an
// update restarts WallpaperAgent, which runs the old extension until then.
func (a *application) checkExtension(updated bool, version string) {
	go func() {
		appex, err := extensionBundle()
		if err == nil {
			err = reloadExtension(appex)
		}
		mygo.RunOnMain(func() {
			if updated {
				a.repairWallpaper("已更新到 "+version+"，正在重新载入壁纸", time.Now())
			}
			if err != nil {
				a.status = "登记壁纸扩展失败：" + err.Error()
			}
			a.invalidate()
		})
	}()
}

// relaunchGuard is the script of /bin/sh that, given the app's pid, its
// bundle, the command that opens it, how many seconds to wait and how many
// fifths of a second to give up after, opens the app once it has exited
// unless the app is running again by then: an updated app relaunches
// itself, which failed once in seven trials. Giving up keeps an app the
// user quits later from opening again.
const relaunchGuard = `pid=$1 app=$2 opener=$3 delay=$4 limit=$5
trap '' HUP
i=0
while kill -0 "$pid" 2>/dev/null; do
	i=$((i+1)); [ "$i" -gt "$limit" ] && exit 0
	sleep 0.2
done
sleep "$delay"
pgrep -qf "$app/Contents/MacOS/" || exec "$opener" "$app"
`

// startRelaunchGuard runs relaunchGuard for the app of pid at bundle,
// detached so that it outlives the app.
func startRelaunchGuard(pid int, bundle, opener string, delay time.Duration, limit time.Duration) error {
	cmd := exec.Command("/bin/sh", "-c", relaunchGuard, "loopwall-relaunch", strconv.Itoa(pid), bundle, opener,
		strconv.FormatFloat(delay.Seconds(), 'f', 1, 64), strconv.Itoa(int(limit/(200*time.Millisecond))))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// guardRelaunch watches over the relaunch into an update, once it is
// installed: if the app does not come back within 8 seconds of exiting, in
// the two minutes after, the guard opens it.
func guardRelaunch() {
	if app, err := appBundle(); err == nil {
		_ = startRelaunchGuard(os.Getpid(), app, "/usr/bin/open", 8*time.Second, 2*time.Minute)
	}
}

// updatesUsed is the updater plugin in use, which main sets, and without
// which the plugin's functions panic: in tests.
var updatesUsed bool

// updateDetail describes the version running and what the last check found.
func updateDetail() string {
	text := "当前版本 " + mygo.App.Version() + " · 每天自动检查一次，有新版本时先询问再安装"
	if !updatesUsed {
		return text
	}
	if v := updater.AvailableVersion(); v != "" {
		text = "有新版本 " + v + "，点「检查更新」安装 · 当前版本 " + mygo.App.Version()
	}
	return text
}
