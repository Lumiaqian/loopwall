package main

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo"
)

func TestTrayIconIsAntialiasedTemplatePNG(t *testing.T) {
	data := trayIcon()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 36 || b.Dy() != 36 {
		t.Fatalf("tray icon size = %dx%d, want 36x36", b.Dx(), b.Dy())
	}
	opaque, semi := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if a < 0xffff {
				semi++
			} else {
				opaque++
			}
			// Non-premultiplied check: pure black RGB for macOS template icon.
			if r != 0 || g != 0 || bl != 0 {
				t.Fatalf("pixel (%d,%d) not black template ink: rgba=(%d,%d,%d,%d)", x, y, r, g, bl, a)
			}
		}
	}
	if opaque == 0 || semi == 0 {
		t.Fatalf("expected anti-aliased template icon with opaque and translucent pixels, got opaque=%d semi=%d", opaque, semi)
	}
}

func TestBuildTrayMenuDisplayStatesAndPowerSaving(t *testing.T) {
	m1 := media(idOne)
	m1.Name = "雪山晨光"
	cfg := catalog.Default()
	cfg.Media = []catalog.MediaEntry{m1}
	cfg.Playlist.PowerSaving = true

	a := &application{
		root:     t.TempDir(),
		host:     &fakeHost{},
		config:   cfg,
		displays: []platform.DisplayInfo{{Identifier: "A", Name: "内建显示器"}, {Identifier: "B", Name: "Studio Display"}},
		runtime: catalog.Runtime{
			SchemaVersion:     1,
			UpdatedAtUnix:     float64(time.Now().Unix()),
			Displays:          []catalog.DisplayProgress{{DisplayKey: "A", VideoID: idOne}},
			ActiveDisplayKeys: []string{"A"},
		},
	}

	menu, sig1 := a.buildTrayMenu()
	items := menu.Items()
	if items[0].Label != "内建显示器" || items[0].Subtitle != "雪山晨光" || items[0].Badge != "播放中" {
		t.Fatalf("first display item = %+v", items[0])
	}
	if items[1].Label != "Studio Display" || items[1].Subtitle != "" || items[1].Badge != "未启用" {
		t.Fatalf("second display item = %+v", items[1])
	}
	var powerItem *mygo.MenuItem
	for _, it := range items {
		if it.Label == "省电模式" {
			powerItem = it
			break
		}
	}
	if powerItem == nil || powerItem.Type != mygo.MenuItemCheckbox || !powerItem.Checked || !powerItem.KeepOpen {
		t.Fatalf("power saving item = %+v", powerItem)
	}

	// Changing power saving changes signature and unchecked state.
	a.setPowerSaving(false)
	menu2, sig2 := a.buildTrayMenu()
	if sig1 == sig2 {
		t.Fatal("tray signature did not change after toggling power saving")
	}
	for _, it := range menu2.Items() {
		if it.Label == "省电模式" && it.Checked {
			t.Fatal("power saving item remained checked after setPowerSaving(false)")
		}
	}
}
