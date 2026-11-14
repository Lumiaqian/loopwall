package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPrefsTheme(t *testing.T) {
	light, dark, desktop := ui.LightTheme(), ui.DarkTheme(), ui.Preferences{}
	if th := (prefs{}).theme(dark, desktop); !th.Dark || th.Accent == dark.Accent {
		t.Errorf("default: dark=%v, accent %v not the app's", th.Dark, th.Accent)
	}
	if th := (prefs{Appearance: "light"}).theme(dark, desktop); th.Dark {
		t.Error("light appearance kept the dark theme")
	}
	if th := (prefs{Appearance: "dark"}).theme(light, desktop); !th.Dark {
		t.Error("dark appearance kept the light theme")
	}
	if th := (prefs{Accent: "system"}).theme(light, desktop); th.Accent != light.Accent {
		t.Errorf("system accent changed the theme's: %v", th.Accent)
	}
	if th := (prefs{Accent: "green"}).theme(light, desktop); th.Accent == light.Accent {
		t.Error("green accent not applied")
	}
}

func TestLookFor(t *testing.T) {
	for key, want := range map[string]string{"": "标准", "none": "无", "max": "极致", "unknown": "标准"} {
		if got := lookFor(key).label; got != want {
			t.Errorf("lookFor(%q) = %s, want %s", key, got, want)
		}
	}
}

func TestMoveFiles(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "Loopwall")
	for name, body := range map[string]string{"settings.json": "old", "share.json": "old", "other": "x"} {
		if err := os.WriteFile(filepath.Join(from, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(to, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "share.json"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := moveFiles(from, to, userDataFiles...); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"settings.json": "old", "share.json": "new"} {
		if b, _ := os.ReadFile(filepath.Join(to, name)); string(b) != want {
			t.Errorf("%s: %q, want %q", name, b, want)
		}
	}
	if _, err := os.Stat(filepath.Join(to, "other")); err == nil {
		t.Error("moved a file not the app's")
	}
}
