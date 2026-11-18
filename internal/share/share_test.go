package share

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

const videoID = "11111111-1111-4111-8111-111111111111"

// library makes a library at a temporary root sharing one video.
func library(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	c := catalog.Default()
	m := catalog.MediaEntry{ID: videoID, Name: "海边", Filename: "video.mp4", Width: 3840, Height: 2160, DurationSeconds: 12, FrameRate: 60}
	c.Media = append(c.Media, m)
	if err := catalog.Save(root, &c); err != nil {
		t.Fatal(err)
	}
	video := bytes.Repeat([]byte("loopwall"), 100_000)
	if err := os.MkdirAll(filepath.Dir(catalog.VideoPath(root, m)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.VideoPath(root, m), video, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.ThumbnailPath(root, m), []byte("jpeg"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, video
}

func open(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// serve starts a server of the library at root on the loopback.
func serve(t *testing.T, root string) (*Server, *Store, Peer) {
	t.Helper()
	st := open(t)
	s := NewServer(root, st)
	s.failDelay = 0
	port, err := s.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, st, Peer{Name: "书房的 Mac", Device: st.Device(), Addr: Addr("127.0.0.1", int64(port))}
}

func TestPairListDownload(t *testing.T) {
	ctx := context.Background()
	root, video := library(t)
	s, served, peer := serve(t, root)
	client := NewClient(open(t), "客厅的 Mac")

	if _, err := client.Library(ctx, peer); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("unpaired: %v, want ErrNotPaired", err)
	}
	if err := client.Pair(ctx, peer, "not-it"); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("a wrong code: %v", err)
	}
	code := s.Code()
	if err := client.Pair(ctx, peer, code); err != nil {
		t.Fatal(err)
	}
	if s.Code() == code {
		t.Error("the code did not turn over after pairing")
	}
	if g := served.Granted()[client.store.Device()]; g.Name != "客厅的 Mac" || g.TokenHash == "" {
		t.Errorf("granted: %+v", g)
	}

	items, err := client.Library(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != videoID || items[0].Name != "海边" || items[0].Ext != ".mp4" || items[0].Size != int64(len(video)) {
		t.Fatalf("library: %+v", items)
	}
	thumb, err := client.Thumbnail(ctx, peer, videoID)
	if err != nil || string(thumb) != "jpeg" {
		t.Fatalf("thumbnail: %q, %v", thumb, err)
	}
	var got bytes.Buffer
	var last int64
	if err = client.Download(ctx, peer, videoID, &got, func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), video) || last != int64(len(video)) {
		t.Fatalf("downloaded %d bytes, reported %d, of %d", got.Len(), last, len(video))
	}
	// Only the library's videos, by ID: no path of the request's.
	if err = client.Download(ctx, peer, "..%2Fcatalog.json", &got, nil); err == nil {
		t.Error("served what is not a video of the library")
	}

	// Revoked, the device pulls nothing more.
	if err = served.Revoke(client.store.Device()); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Library(ctx, peer); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("revoked: %v, want ErrNotPaired", err)
	}
}

func TestWrongCodesVoidTheCode(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, _, peer := serve(t, root)
	client := NewClient(open(t), "x")
	code := s.Code()
	for range maxFailures {
		if err := client.Pair(ctx, peer, "000000x"); !errors.Is(err, ErrWrongCode) {
			t.Fatal(err)
		}
	}
	if s.Code() != "" {
		t.Fatal("the code survived the wrong ones")
	}
	if err := client.Pair(ctx, peer, code); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("the voided code paired: %v", err)
	}
	s.NewCode()
	if err := client.Pair(ctx, peer, s.Code()); err != nil {
		t.Fatalf("a new code: %v", err)
	}
}

func TestChangedCertificate(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, _, peer := serve(t, root)
	client := NewClient(open(t), "x")
	if err := client.Pair(ctx, peer, s.Code()); err != nil {
		t.Fatal(err)
	}
	p, _ := client.store.Paired(peer.Device)
	p.Fingerprint = "another"
	if err := client.store.Pair(peer.Device, p); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Library(ctx, peer); !errors.Is(err, ErrChangedCertificate) {
		t.Fatalf("another certificate: %v, want ErrChangedCertificate", err)
	}
}

func TestStoreKeepsItsState(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.RecordImport("dev", videoID, "local"); err != nil {
		t.Fatal(err)
	}
	cert, err := st.Certificate()
	if err != nil {
		t.Fatal(err)
	}
	again, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Device() != st.Device() {
		t.Error("the device changed")
	}
	if local, ok := again.Imported("dev", videoID); !ok || local != "local" {
		t.Errorf("imported: %q %v", local, ok)
	}
	cert2, err := again.Certificate()
	if err != nil || Fingerprint(cert2.Certificate[0]) != Fingerprint(cert.Certificate[0]) {
		t.Errorf("the certificate changed: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "share-key.pem")); err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("the key's mode: %v %v", info, err)
	}
}

// TestPairingIsMutual checks that one code pairs two Macs both ways: the
// Mac that typed it pulls from the other, the other pulls from it without
// a code of its own; that a failed pairing grants nothing; and that
// unpairing on either side forgets both ways.
func TestPairingIsMutual(t *testing.T) {
	ctx := context.Background()
	rootA, _ := library(t)
	rootB, videoB := library(t)
	serverA, storeA, peerA := serve(t, rootA)
	_, storeB, peerB := serve(t, rootB)
	clientA, clientB := NewClient(storeA, "客厅的 Mac"), NewClient(storeB, "书房的 Mac")

	// A wrong code grants nothing in return.
	if err := clientB.Pair(ctx, peerA, "wrong"); !errors.Is(err, ErrWrongCode) {
		t.Fatal(err)
	}
	if _, ok := storeB.granted(storeA.Device()); ok {
		t.Fatal("a failed pairing left A granted on B")
	}

	// B types A's code: B pulls from A, and A from B.
	if err := clientB.Pair(ctx, peerA, serverA.Code()); err != nil {
		t.Fatal(err)
	}
	if _, err := clientB.Library(ctx, peerA); err != nil {
		t.Fatalf("B pulls from A: %v", err)
	}
	items, err := clientA.Library(ctx, peerB)
	if err != nil {
		t.Fatalf("A pulls from B without a code: %v", err)
	}
	var got bytes.Buffer
	if err = clientA.Download(ctx, peerB, items[0].ID, &got, nil); err != nil || !bytes.Equal(got.Bytes(), videoB) {
		t.Fatalf("A downloads from B: %v", err)
	}

	// A unpairs: both forget both ways.
	if err = clientA.Unpair(ctx, storeB.Device(), peerB.Addr); err != nil {
		t.Fatal(err)
	}
	if _, ok := storeA.Paired(storeB.Device()); ok {
		t.Error("A still pulls from B")
	}
	if _, ok := storeB.Paired(storeA.Device()); ok {
		t.Error("B still pulls from A")
	}
	if _, err = clientB.Library(ctx, peerA); !errors.Is(err, ErrNotPaired) {
		t.Errorf("B after A unpaired: %v, want ErrNotPaired", err)
	}
	if _, err = clientA.Library(ctx, peerB); !errors.Is(err, ErrNotPaired) {
		t.Errorf("A after unpairing: %v, want ErrNotPaired", err)
	}
	// Unpairing a peer that is gone still forgets it here.
	serverA.NewCode()
	if err = clientB.Pair(ctx, peerA, serverA.Code()); err != nil {
		t.Fatal(err)
	}
	if err = clientB.Unpair(ctx, storeA.Device(), ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := storeB.Paired(storeA.Device()); ok {
		t.Error("B still pairs with A after unpairing offline")
	}
}

// TestOldPeerPairsOneWay checks that a pairing request without access in
// return, as versions before 0.1.3 send, still pairs one way.
func TestOldPeerPairsOneWay(t *testing.T) {
	root, _ := library(t)
	s, st, peer := serve(t, root)
	old := open(t)
	body := `{"device":"` + old.Device() + `","name":"旧版","code":"` + s.Code() + `"}`
	hc := httpClient(func(string) error { return nil }, 5*time.Second)
	resp, err := hc.Post("https://"+peer.Addr+"/v1/pair", "application/json", strings.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("pair: %v %v", resp, err)
	}
	resp.Body.Close()
	if _, ok := st.granted(old.Device()); !ok {
		t.Error("the old device is not granted")
	}
	if _, ok := st.Paired(old.Device()); ok {
		t.Error("paired back with a device that granted nothing")
	}
}
