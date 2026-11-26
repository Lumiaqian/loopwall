package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
)

func TestRepairUnregistersOnlyLastSelectedTrashBundle(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Applications/Loopwall.app")
	first := filepath.Join(home, ".Trash/CI-1/Loopwall.app")
	second := filepath.Join(home, ".Trash/CI-2/Loopwall.app")
	line := func(path string) string {
		return "Launching process with config: { bundleID: " + catalog.ExtensionID + ", URL: file://" + filepath.Join(path, extensionRelative) + "/, super: record }\n"
	}
	realCmd, realPlugin := repairCommand, pluginkit
	t.Cleanup(func() { repairCommand, pluginkit = realCmd, realPlugin })
	pluginkit = func(args ...string) ([]byte, error) {
		return []byte("+ id\t" + filepath.Join(app, extensionRelative)), nil
	}
	for _, tc := range []struct {
		name, log, wrongID, live, want string
		fail                           bool
	}{
		{"two choices", line(first) + line(second), "", "", second, false},
		{"official selected last", line(second) + line(app), "", "", "", false},
		{"normal copy", line(filepath.Join(home, "Downloads/Loopwall.app")), "", "", "", false},
		{"other identity", line(second), second, "", "", true},
		{"other running copy", line(second), "", filepath.Join(first, extensionExecutable), "", true},
		{"live installed copy", line(second), "", filepath.Join(app, extensionExecutable), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var removed []string
			registered := false
			repairCommand = func(name string, args ...string) (string, error) {
				switch name {
				case "/usr/bin/log":
					return tc.log, nil
				case "/usr/bin/plutil":
					path := args[len(args)-1]
					if tc.wrongID != "" && strings.HasPrefix(path, tc.wrongID+"/") {
						return "other", nil
					}
					if strings.Contains(path, ".appex/") {
						return catalog.ExtensionID, nil
					}
					return "local.lumiaqian.loopwall", nil
				case "/usr/bin/codesign":
					return "", nil
				case registrar:
					switch args[0] {
					case "-u":
						removed = append(removed, args[1])
					case "-f":
						registered = args[1] == app
					default:
						t.Fatalf("unexpected args %q", args)
					}
					return "", nil
				}
				return "", fmt.Errorf("unexpected command %q", name)
			}
			p := repairProcesses{agent: 50, path: tc.live}
			if tc.live != "" {
				p.extension = 55
			}
			err := prepareRepair(app, home, p)
			if (err != nil) != tc.fail || registered == tc.fail || strings.Join(removed, "|") != tc.want {
				t.Fatalf("removed=%v registered=%v err=%v", removed, registered, err)
			}
		})
	}
}

func TestRepairRequiresBothActiveClocks(t *testing.T) {
	displays := []platform.DisplayInfo{{Identifier: "1", Name: "内建"}, {Identifier: "2", Name: "外接"}}
	report := func(stamp, a, b float64) catalog.Runtime {
		return catalog.Runtime{UpdatedAtUnix: stamp, ActiveDisplayKeys: []string{"1", "2"}, Displays: []catalog.DisplayProgress{{DisplayKey: "1", VideoID: "x", PositionSeconds: a}, {DisplayKey: "2", VideoID: "y", PositionSeconds: b}}}
	}
	prev := report(100, 2, 7)
	for _, tc := range []struct {
		cur catalog.Runtime
		ok  bool
	}{{report(100, 3, 8), false}, {report(110, 3, 7), false}, {report(110, 3, 8), true}} {
		if msg, ok := repairProgress(displays, prev, tc.cur); ok != tc.ok {
			t.Fatal(msg)
		}
	}
	cur := report(110, 3, 8)
	cur.ActiveDisplayKeys = []string{"1"}
	if msg, ok := repairProgress(displays, prev, cur); ok || !strings.Contains(msg, "外接：等待") {
		t.Fatal(msg)
	}
}

func TestRepairReportsIntentionalHolds(t *testing.T) {
	for _, tc := range []struct {
		report catalog.Runtime
		want   string
	}{
		{catalog.Runtime{DisplaysAsleep: true}, "休眠"}, {catalog.Runtime{ScreenLocked: true}, "锁定"}, {catalog.Runtime{PowerSavingActive: true}, "省电"},
	} {
		msg, finished := repairProgress(nil, catalog.Runtime{}, tc.report)
		if !finished || !strings.Contains(msg, tc.want) || strings.Contains(msg, "已推进") {
			t.Fatal(msg)
		}
	}
}

func TestFreshRepairSample(t *testing.T) {
	now := time.Unix(1000, 0)
	since := now.Add(-5 * time.Second)
	for _, offset := range []time.Duration{-6 * time.Second, -5 * time.Second, time.Second} {
		if freshRepairSample(now.Add(offset), since, now) {
			t.Fatalf("accepted %v", offset)
		}
	}
	if !freshRepairSample(now.Add(-time.Second), since, now) {
		t.Fatal("new sample rejected")
	}
}
