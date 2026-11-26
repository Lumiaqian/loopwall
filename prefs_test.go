package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/share"
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

// TestMigrateUserDataPartially checks a migration that finds some files
// already at the destination: those stay, the rest move, nothing is
// lost, and it is safe to run again.
func TestMigrateUserDataPartially(t *testing.T) {
	support := t.TempDir()
	legacy, dir := filepath.Join(support, legacyUserData), filepath.Join(support, "Loopwall")
	write := func(d, name, body string) {
		t.Helper()
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(d, name string) string {
		b, err := os.ReadFile(filepath.Join(d, name))
		if err != nil {
			return "<missing>"
		}
		return string(b)
	}
	write(legacy, "settings.json", "old settings")
	write(legacy, "share.json", "old share")
	write(legacy, "share-cert.pem", "old cert") // no key beside it
	write(dir, "share.json", "new share")
	write(dir, "settings.json", "new settings")

	for run := 0; run < 2; run++ {
		if err := migrateUserDataTo(dir); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]string{"settings.json": "new settings", "share.json": "new share", "share-cert.pem": "old cert", "share-key.pem": "<missing>"} {
			if got := read(dir, name); got != want {
				t.Errorf("run %d: %s = %q, want %q", run, name, got, want)
			}
		}
	}
	// What was at the destination already is left, with its legacy twin.
	if got := read(legacy, "share.json"); got != "old share" {
		t.Errorf("legacy twin = %q", got)
	}
	if got := read(legacy, "share-cert.pem"); got != "<missing>" {
		t.Errorf("a moved file is still in the legacy directory: %q", got)
	}
}

func TestMigrateUserDataWithoutLegacy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Loopwall")
	if err := migrateUserDataTo(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("created a directory with nothing to move")
	}
}

func TestMoveFilesReportsFailure(t *testing.T) {
	from, base := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "settings.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(base, "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The destination cannot be made: the file stays where it is.
	if err := moveFiles(from, filepath.Join(blocked, "Loopwall"), "settings.json"); err == nil {
		t.Fatal("no error for a destination that cannot be made")
	}
	if _, err := os.Stat(filepath.Join(from, "settings.json")); err != nil {
		t.Errorf("the source was lost: %v", err)
	}
}

func TestRetrySharedPreferences(t *testing.T) {
	old := writePrefs
	defer func() { writePrefs = old }()
	store, err := share.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := share.NewServer(t.TempDir(), store)
	if _, err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	server.SetLegacy(true)
	a := application{host: &fakeHost{}, prefs: prefs{Share: true, ShareLegacy: true}, share: &sharing{server: server}}
	writePrefs = func(p prefs) error { return errors.New("disk full") }
	a.setLegacyPairing(false)
	if server.Legacy() {
		t.Fatal("legacy pairing still active")
	}
	a.setSharing(false)
	if server.Port() != 0 {
		t.Fatal("server still active")
	}
	if a.shareSaveError == "" {
		t.Fatal("missing persistence warning")
	}
	saved := prefs{Share: true, ShareLegacy: true}
	writePrefs = func(p prefs) error { saved = p; return nil }
	a.saveShareSettings()
	if a.shareSaveError != "" || saved.Share || saved.ShareLegacy {
		t.Fatal("privacy settings retry failed")
	}
}
