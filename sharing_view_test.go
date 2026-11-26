package main

import (
	"strings"
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
	if _, ok := tt.Find(first); ok {
		t.Fatal("manual code visible before disclosure")
	}
	if err := tt.Click("显示完整配对码"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{first, second, "复制", "换一个", "旧版兼容选项", "附近有同名的 Mac，请核对配对码后再配对", legacyTypedWarning} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the views do not show %q", want)
		}
	}
	if _, ok := tt.Find("允许旧版 Loopwall 配对（不安全）"); ok {
		t.Error("the legacy toggle is expanded by default while ShareLegacy is off")
	}
	if _, ok := tt.Find("旧版配对码"); ok {
		t.Error("the legacy code is shown while legacy pairing is off")
	}
	// Clicking the disclosure reveals the legacy toggle without turning it on.
	if err := tt.Click("旧版兼容选项"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("允许旧版 Loopwall 配对（不安全）"); !ok {
		t.Error("clicking 旧版兼容选项 did not expand the legacy toggle")
	}
	// Allowed, the section stays expanded automatically with its warning and code.
	a.prefs.ShareLegacy = true
	a.share.showLegacyPairing = false
	tt = ui.NewTester(func(c *ui.Context) { a.shareSettings(c) }, 900, 1200)
	if _, ok := tt.Find("已开启（不安全）"); !ok {
		t.Error("the enabled legacy state label is not shown")
	}
	if _, ok := tt.Find("允许旧版 Loopwall 配对（不安全）"); !ok {
		t.Error("the legacy toggle did not auto-expand while ShareLegacy is on")
	}
	if _, ok := tt.Find("旧版配对码  " + srv.Code()[:3] + " " + srv.Code()[3:]); !ok {
		t.Error("the legacy code is not shown while legacy pairing is on")
	}
	if _, ok := tt.Find("6 位数字无法识别冒充者：同一网络里的恶意设备可以冒充这台 Mac，骗走输入的数字并拿到资料库的读取权限。"); !ok {
		t.Error("the legacy warning is not shown")
	}
}

func TestPairingInputHelpers(t *testing.T) {
	if got := cleanPairingInput("  ABCD-EFGH-IJKL-MNOP\r\nQRST-UVWX-2345-6723  "); got != "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-2345-6723" {
		t.Fatalf("cleanPairingInput multiline = %q", got)
	}
	if got := cleanPairingInput("  596 355 \n"); got != "596 355" {
		t.Fatalf("cleanPairingInput single line = %q", got)
	}
	if got := sanitizePairingLive("  596 355 "); got != "596 355 " {
		t.Fatalf("sanitizePairingLive kept trailing space? got %q", got)
	}

	valid, err := share.EncodePairCode("0123456789abcdef0123456789abcdef", "596355")
	if err != nil {
		t.Fatal(err)
	}
	typo := "X" + valid[1:]
	if typo == valid {
		typo = "Y" + valid[1:]
	}
	cases := []struct {
		name      string
		raw       string
		wantKind  pairingHintKind
		wantSub   bool
		wantSubst string
	}{
		{"empty", "   ", pairingHintNone, false, ""},
		{"legacy", "596 355", pairingHintWarning, true, legacyTypedWarning},
		{"valid v2", valid, pairingHintValid, true, "配对码格式正确"},
		{"typo v2", typo, pairingHintWarning, false, share.ErrCodeTypo.Error()},
		{"bad char", "ABCD@1234", pairingHintWarning, false, "只能包含字母"},
		{"too long", valid + "AAAA", pairingHintWarning, false, "超出 32 位"},
		{"digits progress", "1234", pairingHintProgress, false, "4 / 32"},
		{"alpha progress", "ABCD-EFGH", pairingHintProgress, false, "8 / 32"},
	}
	for _, tc := range cases {
		kind, text, can := pairingInputFeedback(tc.raw)
		if kind != tc.wantKind || can != tc.wantSub || (tc.wantSubst != "" && !strings.Contains(text, tc.wantSubst)) {
			t.Errorf("%s: got (%v, %q, %v), want (%v, *%q*, %v)", tc.name, kind, text, can, tc.wantKind, tc.wantSubst, tc.wantSub)
		}
	}
}

func TestPairingNarrowLayoutPasteTypoAndCopyFeedback(t *testing.T) {
	st, err := share.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := share.NewServer(t.TempDir(), st)
	a := &application{prefs: prefs{Share: true}}
	a.share = &sharing{
		store:   st,
		server:  srv,
		client:  share.NewClient(st, "本机"),
		name:    "本机",
		remotes: map[string]*remote{},
		pulls:   map[string]*pull{},
		codes:   map[string]*string{},
		pasted:  map[string]string{},
		seen:    map[string]time.Time{},
		pairing: map[string]bool{},
		status:  platform.SharingStatus{Advertised: true},
	}
	p := share.Peer{Name: "客厅 Mac", Device: "peer-1", Addr: "10.0.0.2:9000"}
	a.share.peers = []share.Peer{p}

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(20).Gap(10).Children(func() {
			a.shareSettings(c)
			a.peerSection(c, p)
		})
	}, 760, 1200)

	// Narrow window check: input and buttons stay within the 760px viewport.
	if r, ok := tt.Find("配对"); !ok || r.X+r.W > 760 {
		t.Fatalf("pair button out of 760px bounds: ok=%v rect=%+v", ok, r)
	}

	// Copy button flips to "已复制" in-place and reverts after 2 seconds.
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("已复制"); !ok {
		t.Fatal("copy button did not show transient 已复制")
	}
	if tt.Clipboard() != srv.PairingCode() {
		t.Fatalf("clipboard = %q, want %q", tt.Clipboard(), srv.PairingCode())
	}
	tt.Advance(2*time.Second + 50*time.Millisecond)
	if _, ok := tt.Find("复制"); !ok {
		t.Fatal("copy button did not revert to 复制 after 2s")
	}

	// Paste cleans multiline code from clipboard and validates live.
	first, second := codeLines(srv.PairingCode())
	tt.SetClipboard("  " + first + "\n" + second + "  ")
	if err := tt.Click("粘贴"); err != nil {
		t.Fatal(err)
	}
	if *a.share.code(p.Device) != srv.PairingCode() {
		t.Fatalf("pasted code = %q, want %q", *a.share.code(p.Device), srv.PairingCode())
	}
	if _, ok := tt.Find("配对码格式正确"); !ok {
		t.Fatal("valid pasted code did not show 配对码格式正确")
	}
	if info, ok := tt.Access("配对"); !ok || info.Disabled {
		t.Fatalf("pair button should be enabled for valid code: ok=%v info=%+v", ok, info)
	}

	// Mistyped code shows live warning and disables submit.
	*a.share.code(p.Device) = "X" + srv.PairingCode()[1:]
	if *a.share.code(p.Device) == srv.PairingCode() {
		*a.share.code(p.Device) = "Y" + srv.PairingCode()[1:]
	}
	tt.Frame()
	if _, ok := tt.Find(share.ErrCodeTypo.Error()); !ok {
		t.Fatal("typo code did not show ErrCodeTypo hint live")
	}
	if info, ok := tt.Access("配对"); !ok || !info.Disabled {
		t.Fatalf("pair button should be disabled for typo code: ok=%v info=%+v", ok, info)
	}
}
