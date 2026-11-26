package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo"
)

const registrar = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
const agentExecutable = "/System/Library/CoreServices/WallpaperAgent.app/Contents/MacOS/WallpaperAgent"
const extensionRelative = "Contents/Extensions/LoopwallExtension.appex"
const extensionExecutable = extensionRelative + "/Contents/MacOS/LoopwallExtension"

var repairCommand = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w %s", filepath.Base(name), err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

// Read only the launch identity emitted by ExtensionKit for this user's current
// agent. pluginkit may list only the installed bundle even on a failed launch.
func selectedExtension(log string) string {
	const marker = "bundleID: local.lumiaqian.loopwall.extension, URL: "
	var selected string
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "Launching process with config:") {
			continue
		}
		selected = "" // Only the latest launch record is authoritative.
		_, tail, ok := strings.Cut(line, marker)
		if !ok {
			continue
		}
		raw, _, ok := strings.Cut(tail, ", super:")
		u, err := url.Parse(raw)
		if ok && err == nil && u.Scheme == "file" && u.Host == "" && u.RawQuery == "" && u.Fragment == "" && filepath.IsAbs(u.Path) {
			selected = filepath.Clean(u.Path)
		}
	}
	return selected
}

func trashBundle(ext, home string) string {
	if ext == "" {
		return ""
	}
	app, ok := strings.CutSuffix(ext, "/"+extensionRelative)
	if !ok {
		return ""
	}
	trash := filepath.Join(home, ".Trash") + string(os.PathSeparator)
	if !strings.HasPrefix(app, trash) || filepath.Ext(app) != ".app" || filepath.Clean(ext) != ext || canonicalPath(ext) != ext {
		return ""
	}
	return app
}

func verifyRepairBundle(app string) error {
	for _, bundle := range []struct{ path, id string }{{app, "local.lumiaqian.loopwall"}, {filepath.Join(app, extensionRelative), catalog.ExtensionID}} {
		id, err := repairCommand("/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", filepath.Join(bundle.path, "Contents/Info.plist"))
		if err != nil || strings.TrimSpace(id) != bundle.id {
			return fmt.Errorf("无法核实 Loopwall 身份：%s", bundle.path)
		}
	}
	_, err := repairCommand("/usr/bin/codesign", "--verify", "--deep", "--strict", app)
	return err
}

type repairProcesses struct {
	agent, extension int
	path             string
}

func currentRepairProcesses() (repairProcesses, error) {
	var p repairProcesses
	out, err := repairCommand("/bin/ps", "-axww", "-o", "uid=,pid=,comm=")
	if err != nil {
		return p, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != strconv.Itoa(os.Getuid()) {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid <= 1 {
			continue
		}
		// Preserve embedded whitespace in executable paths.
		path := strings.TrimSpace(line)
		path = strings.TrimSpace(strings.TrimPrefix(path, fields[0]))
		path = strings.TrimSpace(strings.TrimPrefix(path, fields[1]))
		switch {
		case path == agentExecutable:
			p.agent = pid
		case filepath.Base(path) == "LoopwallExtension":
			if p.extension != 0 {
				return p, errors.New("有多个壁纸扩展正在运行，无法确定来源")
			}
			p.extension, p.path = pid, path
		}
	}
	return p, nil
}

func launchSelection(agent int) string {
	predicate := fmt.Sprintf(`processIdentifier == %d AND subsystem == "com.apple.extensionkit" AND category == "launch" AND eventMessage CONTAINS "%s"`, agent, catalog.ExtensionID)
	out, _ := repairCommand("/usr/bin/log", "show", "--last", "30m", "--style", "compact", "--predicate", predicate)
	return selectedExtension(out)
}

// Only an installation running from an Applications directory can select
// itself as the repair target. A development copy cannot displace it.
func prepareRepair(app, home string, p repairProcesses) error {
	if app != "/Applications/Loopwall.app" && app != filepath.Join(home, "Applications/Loopwall.app") {
		return errors.New("请从应用程序目录打开正式版 Loopwall")
	}
	if canonicalPath(filepath.Join(app, extensionRelative)) != filepath.Join(app, extensionRelative) {
		return errors.New("正式版扩展路径包含重定向")
	}
	if p.agent == 0 {
		return errors.New("未找到当前用户的墙纸服务")
	}
	if p.extension != 0 && p.path != filepath.Join(app, extensionExecutable) {
		return errors.New("其他位置的壁纸扩展正在运行，已保留该副本")
	}
	if err := verifyRepairBundle(app); err != nil {
		return err
	}
	if p.extension == 0 {
		selected := launchSelection(p.agent)
		if old := trashBundle(selected, home); old != "" {
			if err := verifyRepairBundle(old); err != nil {
				return err
			}
			if _, err := repairCommand(registrar, "-u", old); err != nil {
				return err
			}
		}
	}
	if _, err := repairCommand(registrar, "-f", app); err != nil {
		return err
	}
	ext := filepath.Join(app, extensionRelative)
	if err := reloadExtension(ext); err != nil {
		return err
	}
	out, err := pluginkit("-m", "-v", "-i", catalog.ExtensionID)
	if err != nil {
		return err
	}
	if !registeredAt(out, ext) {
		return errors.New("系统尚未确认正式版扩展登记")
	}
	return nil
}

var beginWallpaperRepair = func(a *application) {
	displays := slices.Clone(a.displays)
	root := a.root
	go func() {
		result := runWallpaperRepair(root, displays)
		mygo.RunOnMain(func() {
			a.repairing = false
			a.repairedAt = time.Now()
			a.status = result
			a.invalidate()
		})
	}()
}

func (a *application) startWallpaperRepair() {
	if a.repairing {
		a.status = "正在验证各屏播放进度，请稍候"
		return
	}
	if time.Since(a.repairedAt) < repairSettle {
		a.status = "墙纸服务刚完成修复操作，请稍后再试"
		return
	}
	a.repairedAt = time.Now()
	a.repairing = true
	a.status = "正在核对正式版登记和各屏播放状态…"
	beginWallpaperRepair(a)
}

func runWallpaperRepair(root string, displays []platform.DisplayInfo) string {
	app, err := appBundle()
	if err != nil {
		return err.Error()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err.Error()
	}
	before, err := currentRepairProcesses()
	if err != nil {
		return err.Error()
	}
	if err = prepareRepair(app, home, before); err != nil {
		return "修复未重启墙纸服务：" + err.Error()
	}
	current, err := currentRepairProcesses()
	if err != nil || current != before {
		return "墙纸进程已变化，请稍后再试"
	}
	started := time.Now()
	if err = syscall.Kill(before.agent, syscall.SIGTERM); err != nil {
		return "重启墙纸服务失败：" + err.Error()
	}
	var previous catalog.Runtime
	var pid int
	since := started
	detail := "等待系统启动正式版扩展；请确认已在系统墙纸设置中启用 Loopwall，并在亮屏解锁后查看"
	for time.Since(started) < 45*time.Second {
		time.Sleep(2 * time.Second)
		p, err := currentRepairProcesses()
		if err != nil {
			return err.Error()
		}
		if p.extension != 0 && p.path != filepath.Join(app, extensionExecutable) {
			return "系统启动了其他位置的扩展，未确认恢复：" + p.path
		}
		if pid != p.extension {
			previous = catalog.Runtime{}
			pid = p.extension
			since = time.Now()
			detail = "等待系统重新启动扩展，请确认显示器已唤醒、解锁并启用 Loopwall"
		}
		if p.agent == 0 || p.agent == before.agent || pid == 0 || pid == before.extension {
			continue
		}
		r, err := catalog.ReadRuntime(root)
		if err != nil {
			detail = "正式版已启动，无法读取播放报告：" + err.Error()
			continue
		}
		now := time.Now()
		stamp := time.Unix(0, int64(r.UpdatedAtUnix*1e9))
		if !freshRepairSample(stamp, since, now) {
			detail = "正式版已启动，等待重启后的播放报告；若已休眠或锁屏，请唤醒并解锁"
			continue
		}

		if r.UpdatedAtUnix <= previous.UpdatedAtUnix {
			continue
		}
		var done bool
		detail, done = repairProgress(displays, previous, r)
		if done {
			return detail
		}
		previous = r
	}
	p, err := currentRepairProcesses()
	if err == nil && p.agent != 0 && p.agent != before.agent && p.extension == 0 && trashBundle(launchSelection(p.agent), home) != "" {
		return "系统又选择了废纸篓中的旧副本。再次点修复可处理该副本"
	}
	return "验证超时：" + detail
}

func freshRepairSample(stamp, since, now time.Time) bool {
	return stamp.After(since) && !stamp.After(now) && now.Sub(stamp) <= catalog.RuntimeStaleAfter
}

func repairProgress(displays []platform.DisplayInfo, prev, cur catalog.Runtime) (string, bool) {

	switch {
	case cur.DisplaysAsleep:
		return "正式版已启动；显示器休眠，唤醒后可核对播放", true
	case cur.ScreenLocked:
		return "正式版已启动；屏幕已锁定，解锁后可核对桌面播放", true
	case cur.PowerSavingActive:
		return "正式版已启动；省电模式正在暂停播放", true
	}
	if len(displays) == 0 {
		return "等待系统报告已连接的显示器", false
	}
	var messages []string
	all := true
	for _, d := range displays {
		status := "等待系统展示；请确认该屏已唤醒、解锁并启用 Loopwall"
		moving := false
		if slices.Contains(cur.ActiveDisplayKeys, d.Identifier) {
			status = "已活跃，等待播放进度"
			v := activeProgress(prev, d.Identifier)
			c := activeProgress(cur, d.Identifier)
			moving = prev.UpdatedAtUnix < cur.UpdatedAtUnix && v != nil && c != nil && c.VideoID == v.VideoID && (c.PositionSeconds > v.PositionSeconds || c.ItemElapsedSeconds > v.ItemElapsedSeconds)
		}
		if moving {
			status = "播放进度已推进"
		}
		messages = append(messages, d.Name+"："+status)
		all = all && moving
	}
	return strings.Join(messages, "；"), all
}

// Use the extension's measured desktop timeline, never the UI's extrapolated counter.
func activeProgress(r catalog.Runtime, key string) *catalog.DisplayProgress {
	if !slices.Contains(r.ActiveDisplayKeys, key) {
		return nil
	}
	for i := range r.Displays {
		p := &r.Displays[i]
		if p.DisplayKey == key && p.VideoID != "" {
			return p
		}
	}
	return nil
}
