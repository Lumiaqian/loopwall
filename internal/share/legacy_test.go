package share

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oldClientPair is a pairing request as 0.1.3 sends it: no version, no
// client certificate. It returns the status and the refusal's reason.
func oldClientPair(t *testing.T, addr, device, secret string) (int, string) {
	t.Helper()
	hc := httpClient(nil, func(string) error { return nil }, nil, 5*time.Second)
	body := `{"device":"` + device + `","name":"旧版","code":"` + secret + `","back":{"token":"` + randomHex(32) + `","fingerprint":"` + randomHex(32) + `"}}`
	resp, err := hc.Post("https://"+addr+"/v1/pair", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode, resp.Header.Get(errHeader)
}

// TestLegacyPairingIsOffByDefault checks that six digits pair only when
// the user allowed it, and that the refusal is told apart from a wrong
// code by the new client, and looks like one to the old.
func TestLegacyPairingIsOffByDefault(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, served, peer := serve(t, root)
	if s.Legacy() {
		t.Fatal("legacy pairing is on by default")
	}
	secret := s.Code()
	client := NewClient(open(t), "新版")

	if err := client.Pair(ctx, peer, secret); !errors.Is(err, ErrLegacyDenied) {
		t.Fatalf("a new client with the legacy code: %v, want ErrLegacyDenied", err)
	}
	if _, ok := client.store.granted(peer.Device); ok {
		t.Error("the reverse access was left granted after a refusal")
	}
	// An old client: a 403, which it shows as a wrong code, and the secret
	// is not tried, so no guess is counted nor the code used up.
	old := open(t)
	if status, why := oldClientPair(t, peer.Addr, old.Device(), secret); status != http.StatusForbidden || why != errLegacyDenied {
		t.Fatalf("an old client: %d %q, want 403 %q", status, why, errLegacyDenied)
	}
	for range maxFailures + 1 {
		oldClientPair(t, peer.Addr, old.Device(), "000000")
	}
	if !untouched(s, secret) {
		t.Error("refused legacy requests counted against the secret")
	}
	if len(served.Granted()) != 0 {
		t.Error("granted while legacy pairing is off")
	}
	// The long code is not affected.
	if err := client.Pair(ctx, peer, s.PairingCode()); err != nil {
		t.Fatalf("the long code: %v", err)
	}
}

// TestLegacyPairingWhenAllowed checks the legacy path with the setting on:
// an old client, and a new one with six digits, pair; the pairing is
// recorded as legacy, a long code's as verified.
func TestLegacyPairingWhenAllowed(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, served, peer := serve(t, root)
	s.SetLegacy(true)

	old := open(t)
	if status, _ := oldClientPair(t, peer.Addr, old.Device(), s.Code()); status != http.StatusOK {
		t.Fatalf("an old client: %d", status)
	}
	if g := served.Granted()[old.Device()]; g.ClientFingerprint != "" {
		t.Errorf("a legacy grant is bound to %q", g.ClientFingerprint)
	}
	if p, ok := served.Paired(old.Device()); !ok || p.Verified {
		t.Errorf("the reverse of a legacy pairing: %+v %v, want unverified", p, ok)
	}

	legacy := NewClient(open(t), "新版甲")
	if err := legacy.Pair(ctx, peer, s.Code()); err != nil {
		t.Fatal(err)
	}
	if p, ok := legacy.store.Paired(peer.Device); !ok || p.Verified {
		t.Errorf("a pairing by six digits: %+v %v, want it recorded as legacy", p, ok)
	}
	if _, err := legacy.Library(ctx, peer); err != nil {
		t.Fatalf("a legacy pairing pulls: %v", err)
	}
	if g, _ := legacy.store.granted(peer.Device); g.ClientFingerprint != "" {
		t.Errorf("the grant of a legacy pairing is bound to %q", g.ClientFingerprint)
	}

	secure := NewClient(open(t), "新版乙")
	if err := secure.Pair(ctx, peer, s.PairingCode()); err != nil {
		t.Fatal(err)
	}
	if p, _ := secure.store.Paired(peer.Device); !p.Verified {
		t.Errorf("a pairing by the long code: %+v, want it verified", p)
	}
	if p, _ := served.Paired(secure.store.Device()); !p.Verified {
		t.Errorf("the reverse of a long code's pairing: %+v, want it verified", p)
	}
}

// TestOldStateStillLoads checks that a share.json from before these fields
// is kept working as legacy: its pairing is unverified, and its grant is
// honoured by the token alone.
func TestOldStateStillLoads(t *testing.T) {
	dir := t.TempDir()
	old := `{"device":"` + randomHex(16) + `",
	 "granted":{"olddev":{"name":"旧","tokenHash":"` + hashHex("tok") + `","pairedAt":1700000000}},
	 "peers":{"oldpeer":{"name":"旧的 Mac","token":"t","fingerprint":"` + randomHex(32) + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, "share.json"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := st.Paired("oldpeer"); !ok || p.Verified {
		t.Errorf("an old pairing: %+v %v, want it unverified", p, ok)
	}
	if g := st.Granted()["olddev"]; g.ClientFingerprint != "" || g.Name != "旧" {
		t.Errorf("an old grant: %+v", g)
	}

	root, _ := library(t)
	s := NewServer(root, st)
	port, err := s.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	hc := httpClient(nil, func(string) error { return nil }, nil, 5*time.Second)
	req, _ := http.NewRequest(http.MethodGet, "https://"+Addr("127.0.0.1", int64(port))+"/v1/library", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Loopwall-Device", "olddev")
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("an old grant, by its token alone: %d, want 200", resp.StatusCode)
	}
}

// A device paired by the long code is never re-paired the legacy way, even
// with legacy pairing allowed: six digits must not replace its bound grant
// and pinned certificate.
func TestLegacyCannotReplaceVerifiedPairing(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, served, peer := serve(t, root)
	s.SetLegacy(true)

	secure := NewClient(open(t), "新版")
	if err := secure.Pair(ctx, peer, s.PairingCode()); err != nil {
		t.Fatal(err)
	}
	bound := served.Granted()[secure.store.Device()]
	if bound.ClientFingerprint == "" {
		t.Fatal("the long code's grant is not bound")
	}
	status, header := oldClientPair(t, peer.Addr, secure.store.Device(), s.Code())
	if status != http.StatusForbidden || header != errLegacyDenied {
		t.Fatalf("a legacy re-pairing of a verified device: %d %q, want 403 %q", status, header, errLegacyDenied)
	}
	if g := served.Granted()[secure.store.Device()]; g != bound {
		t.Errorf("the verified grant changed: %+v, want %+v", g, bound)
	}
	if p, _ := served.Paired(secure.store.Device()); !p.Verified {
		t.Errorf("the verified pin was replaced: %+v", p)
	}
}
