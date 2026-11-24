package main

import (
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestGlobalShortcutsAndUpdateBadge(t *testing.T) {
	realUsed, realAvail := updatesUsed, availableUpdate
	updatesUsed = true
	availableUpdate = func() string { return "0.1.6" }
	t.Cleanup(func() {
		updatesUsed = realUsed
		availableUpdate = realAvail
	})

	m := media(idOne)
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m}
	a := &application{
		root:    t.TempDir(),
		host:    &fakeHost{},
		config:  cfg,
		section: sectionLibrary,
	}
	tt := ui.NewTester(a.view, 960, 720)

	if _, ok := tt.Find("新版本"); !ok {
		t.Fatal("sidebar missing update badge when availableUpdate returns 0.1.6")
	}

	for _, tc := range []struct {
		key  ui.Key
		want string
	}{
		{ui.Key2, sectionPlay},
		{ui.Key3, sectionNearby},
		{ui.Key4, sectionSetting},
		{ui.Key1, sectionLibrary},
		{ui.KeyComma, sectionSetting},
	} {
		tt.Key(ui.Cmd, tc.key)
		if a.section != tc.want {
			t.Fatalf("Cmd+%v: section=%q, want %q", tc.key, a.section, tc.want)
		}
	}

	tt.Key(ui.Cmd, ui.KeyF)
	if a.section != sectionLibrary || !tt.Focused("搜索资料库") {
		t.Fatalf("Cmd+F did not jump to library and focus search: section=%q focused=%v", a.section, tt.Focused("搜索资料库"))
	}

	// Cmd+O opens import chooser cleanly without setting process toast.
	realOpenDlg := openImportDialog
	openedDlg := make(chan struct{}, 1)
	openImportDialog = func(_ *mygo.Window) ([]string, error) {
		select {
		case openedDlg <- struct{}{}:
		default:
		}
		return nil, nil
	}
	t.Cleanup(func() { openImportDialog = realOpenDlg })
	tt.Key(ui.Cmd, ui.KeyO)
	select {
	case <-openedDlg:
	case <-time.After(time.Second):
		t.Fatal("Cmd+O did not trigger import dialog")
	}
	a.choosingImport = false
}

func TestDisplayCardWidthThumbPreviewAndNearbySpinner(t *testing.T) {
	realStarter := previewStarter
	var previewed catalog.MediaEntry
	previewStarter = func(_ *application, m catalog.MediaEntry) { previewed = m }
	t.Cleanup(func() { previewStarter = realStarter })

	m := media(idOne)
	m.Name = "极光夜空"
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m}

	a := &application{
		root:     t.TempDir(),
		host:     &fakeHost{},
		config:   cfg,
		section:  sectionPlay,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "Studio Display"}},
		runtime: catalog.Runtime{
			SchemaVersion:     1,
			UpdatedAtUnix:     float64(time.Now().Unix()),
			Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: idOne, PositionSeconds: 2}},
			ActiveDisplayKeys: []string{"A"},
		},
	}

	tt := ui.NewTester(a.view, 1280, 760)
	rThumb, ok := tt.Find("预览 极光夜空")
	if !ok {
		t.Fatal("display card preview thumbnail not found")
	}
	if err := tt.Click("预览 极光夜空"); err != nil {
		t.Fatal(err)
	}
	if previewed.ID != idOne {
		t.Fatalf("clicking display card thumbnail previewed %q, want %q", previewed.ID, idOne)
	}
	// Display card content width capped at 480px on wide windows.
	rProg, ok := tt.Find("播放进度")
	if !ok {
		t.Fatal("display card progress bar not found")
	}
	if cardSpan := (rProg.X + rProg.W) - rThumb.X; cardSpan > 480 {
		t.Fatalf("display card span = %.1f, want <= 480", cardSpan)
	}

	// Nearby empty state shows scanning spinner.
	st, err := share.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.share = &sharing{
		store:   st,
		server:  share.NewServer(t.TempDir(), st),
		client:  share.NewClient(st, "本机"),
		name:    "本机",
		remotes: map[string]*remote{},
		pulls:   map[string]*pull{},
		codes:   map[string]*string{},
		pasted:  map[string]string{},
		seen:    map[string]time.Time{},
		pairing: map[string]bool{},
	}
	a.share.browsing.Store(true)
	a.section = sectionNearby
	tt.Frame()
	if _, ok := tt.Find("正在搜索局域网设备"); !ok {
		t.Fatal("nearby empty state missing scanning spinner")
	}
	if _, ok := tt.Find("持续扫描局域网中…"); !ok {
		t.Fatal("nearby empty state missing scanning text")
	}
}
