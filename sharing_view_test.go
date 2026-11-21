package main

import (
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo/ui"
)

// TestShareViewsShowThePairingCode renders the sharing settings and the
// Nearby section headless, to check that the long code, the legacy toggle,
// the same-name warning and the legacy hint are in the views.
func TestShareViewsShowThePairingCode(t *testing.T) {
	st, err := share.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := share.NewServer(t.TempDir(), st)
	a := &application{prefs: prefs{Share: true}}
	a.share = &sharing{store: st, server: srv, client: share.NewClient(st, "本机"), name: "本机",
		remotes: map[string]*remote{}, pulls: map[string]*pull{}, codes: map[string]*string{}, seen: map[string]time.Time{}, pairing: map[string]bool{},
		status: platform.SharingStatus{Advertised: true}}
	a.share.peers = []share.Peer{{Name: "书房", Device: "x", Addr: "10.0.0.1:1"}, {Name: "书房", Device: "y", Addr: "10.0.0.2:1"}}
	*a.share.code("x") = "123456"

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20).Gap(10).Children(func() {
			a.shareSettings(c)
			for _, p := range a.share.peers {
				a.peerSection(c, p)
			}
		})
	}, 900, 1800)
	code := srv.PairingCode()
	first, second := codeLines(code)
	for _, want := range []string{first, second, "复制", "换一个", "允许旧版 Loopwall 配对", "附近有同名的 Mac，请核对配对码后再配对", legacyTypedWarning} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the views do not show %q", want)
		}
	}
	if _, ok := tt.Find("旧版配对码"); ok {
		t.Error("the legacy code is shown while legacy pairing is off")
	}
	// Allowed, the six digits are shown, with their warning.
	a.prefs.ShareLegacy = true
	tt = ui.NewTester(func(c *ui.Context) { a.shareSettings(c) }, 900, 1200)
	if _, ok := tt.Find("旧版配对码  " + srv.Code()[:3] + " " + srv.Code()[3:]); !ok {
		t.Error("the legacy code is not shown while legacy pairing is on")
	}
	if _, ok := tt.Find("6 位数字无法识别冒充者：同一网络里的恶意设备可以冒充这台 Mac，骗走输入的数字并拿到资料库的读取权限。"); !ok {
		t.Error("the legacy warning is not shown")
	}
}
