package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLaunchedVersion checks that only a launch as a version other than
// the one the app last ran as counts as an update, and that each launch
// records its version.
func TestLaunchedVersion(t *testing.T) {
	var written []string
	saved := writePrefs
	writePrefs = func(p prefs) error { written = append(written, p.LastVersion); return nil }
	t.Cleanup(func() { writePrefs = saved })

	a := &application{}
	for _, c := range []struct {
		version string
		updated bool
	}{
		{"0.1.2", false}, // the first launch that records one: installed by hand
		{"0.1.2", false}, // relaunched
		{"0.1.3", true},  // updated
		{"", false},      // a development build has no version
		{"0.1.3", false},
	} {
		if got := a.launchedVersion(c.version); got != c.updated {
			t.Errorf("launched as %q after %v: updated %v, want %v", c.version, written, got, c.updated)
		}
	}
	if !slices.Equal(written, []string{"0.1.2", "0.1.3"}) || a.prefs.LastVersion != "0.1.3" {
		t.Errorf("recorded %v, last %q", written, a.prefs.LastVersion)
	}
}

// TestReloadExtension checks that the extension is registered after an
// update only when PlugInKit lost it.
func TestReloadExtension(t *testing.T) {
	var calls []string
	listed := ""
	saved := pluginkit
	pluginkit = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "-m" {
			return []byte(listed), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { pluginkit = saved })

	listed = "     local.lumiaqian.loopwall.extension(0.1.2)\n"
	if err := reloadExtension("/A/LoopwallExtension.appex"); err != nil || len(calls) != 1 {
		t.Fatalf("registered: %v, calls %v", err, calls)
	}
	calls, listed = nil, "\n"
	if err := reloadExtension("/A/LoopwallExtension.appex"); err != nil || len(calls) != 2 || calls[1] != "-a /A/LoopwallExtension.appex" {
		t.Fatalf("lost: %v, calls %v", err, calls)
	}
	pluginkit = func(args ...string) ([]byte, error) {
		if args[0] == "-m" {
			return nil, nil
		}
		return []byte("no such plug-in"), errors.New("exit status 1")
	}
	if err := reloadExtension("/A/LoopwallExtension.appex"); err == nil {
		t.Error("a failed registration reported none")
	}
}

// TestRelaunchGuard checks the guard of the relaunch into an update: once
// the app exits, it opens the app unless the app runs again; and it gives
// up on an app that keeps running.
func TestRelaunchGuard(t *testing.T) {
	dir := t.TempDir()
	opened := filepath.Join(dir, "opened")
	opener := filepath.Join(dir, "opener")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\necho \"$1\" >> "+opened+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// app runs a process standing in for the app: its pid.
	app := func(seconds string) int {
		cmd := exec.Command("/bin/sleep", seconds)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { _ = cmd.Wait() }()
		return cmd.Process.Pid
	}
	wasOpened := func(within time.Duration) string {
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(opened); err == nil {
				return strings.TrimSpace(string(b))
			}
			time.Sleep(50 * time.Millisecond)
		}
		return ""
	}

	// The app exited and nothing reopened it: the guard does.
	gone := filepath.Join(dir, "Gone.app")
	if err := startRelaunchGuard(app("0.2"), gone, opener, 200*time.Millisecond, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := wasOpened(5 * time.Second); got != gone {
		t.Fatalf("opened %q, want %q", got, gone)
	}
	_ = os.Remove(opened)

	// The app relaunched itself: an executable of its bundle runs.
	back := filepath.Join(dir, "Back.app")
	exe := filepath.Join(back, "Contents", "MacOS", "Back")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	// A script, as a copy of a system executable would not run.
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nsleep \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(exe, "20")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = running.Process.Kill(); _ = running.Wait() })
	if err := startRelaunchGuard(app("0.2"), back, opener, 200*time.Millisecond, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := wasOpened(2 * time.Second); got != "" {
		t.Errorf("opened %q though it runs again", got)
	}
	_ = os.Remove(opened)

	// The app keeps running past the guard's limit: the user quits it later.
	if err := startRelaunchGuard(app("3"), gone, opener, 200*time.Millisecond, 600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := wasOpened(5 * time.Second); got != "" {
		t.Errorf("opened %q after giving up", got)
	}
}
