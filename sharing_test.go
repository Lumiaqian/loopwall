package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
)

func TestSharingOffLeavesNearbyUninitialized(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	a := &application{root: t.TempDir(), host: &fakeHost{}}
	a.setupSharing()
	if a.share == nil {
		t.Fatal(a.status)
	}
	a.configureNearby()
	a.pollNearby()
	if a.share.nearby != nil || a.share.server.Port() != 0 || a.status != "" {
		t.Fatalf("idle sharing initialized Nearby or reported an error: %q", a.status)
	}
}

func TestFriendly(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{share.ErrWrongCode, "配对码不对"},
		{fmt.Errorf("pair: %w", share.ErrFingerprintMismatch), "可能有人冒充"},
		{share.ErrLegacyDenied, "允许旧版 Loopwall 配对"},
		{share.ErrCodeTypo, "配对码有输错的字符"},
		{share.ErrCodeFormat, "长度或格式不对"},
		{fmt.Errorf("get: %w", context.DeadlineExceeded), "连接超时"},
		{fmt.Errorf("dial: %w", syscall.ECONNREFUSED), "已关闭共享"},
		{fmt.Errorf("下载不完整：%w", io.ErrUnexpectedEOF), "连接中断"},
	} {
		if got := friendly(c.err); !strings.Contains(got, c.want) {
			t.Errorf("friendly(%v) = %q, want it to say %q", c.err, got, c.want)
		}
	}
}

// TestSharingNotes checks that what Bonjour reports reaches the words the
// settings and the Nearby page show: a failed or denied publish or browse
// is a problem, never "searching" forever.
func TestSharingNotes(t *testing.T) {
	for _, c := range []struct {
		st      platform.SharingStatus
		want    string
		problem bool
	}{
		{platform.SharingStatus{}, "正在发布", false},
		{platform.SharingStatus{Advertised: true}, "「书房的 Mac」", false},
		{platform.SharingStatus{AdvertiseError: "本地网络权限未开启", Denied: true}, "本地网络权限未开启", true},
	} {
		got, problem := publishNote(c.st, "书房的 Mac")
		if !strings.Contains(got, c.want) || problem != c.problem {
			t.Errorf("publishNote(%+v) = %q, %v; want %q, %v", c.st, got, problem, c.want, c.problem)
		}
	}
	for _, c := range []struct {
		st   platform.SharingStatus
		want string
	}{
		{platform.SharingStatus{}, ""},
		{platform.SharingStatus{Browsing: "ready"}, ""},
		{platform.SharingStatus{Browsing: "waiting", BrowseError: "本地网络权限未开启"}, "本地网络权限未开启"},
		{platform.SharingStatus{Browsing: "failed"}, "等待网络"},
	} {
		if got := browseProblem(c.st); got != c.want {
			t.Errorf("browseProblem(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}

// TestMergePeers checks that a Mac Bonjour stops reporting stays listed
// for a while, rather than flicker away, and then goes; and that what is
// found now replaces what was, as a peer whose port changed.
func TestMergePeers(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	seen := map[string]time.Time{}
	a := share.Peer{Name: "甲", Device: "a", Addr: "10.0.0.1:1"}
	b := share.Peer{Name: "乙", Device: "b", Addr: "10.0.0.2:1"}
	names := func(ps []share.Peer) string {
		var s []string
		for _, p := range ps {
			s = append(s, p.Name+"@"+p.Addr)
		}
		return strings.Join(s, ",")
	}
	shown := mergePeers(nil, []share.Peer{b, a}, seen, now)
	if got := names(shown); got != "乙@10.0.0.2:1,甲@10.0.0.1:1" && got != "甲@10.0.0.1:1,乙@10.0.0.2:1" {
		t.Fatalf("found both: %s", got)
	}
	// b drops out for a tick, and a moves to another port.
	a2 := a
	a2.Addr = "10.0.0.1:2"
	shown = mergePeers(shown, []share.Peer{a2}, seen, now.Add(2*time.Second))
	if got := names(shown); !strings.Contains(got, "乙@10.0.0.2:1") || !strings.Contains(got, "甲@10.0.0.1:2") || len(shown) != 2 {
		t.Fatalf("after b dropped out once: %s", got)
	}
	// b stays gone past the grace.
	shown = mergePeers(shown, []share.Peer{a2}, seen, now.Add(peerGrace+time.Second))
	if got := names(shown); got != "甲@10.0.0.1:2" {
		t.Fatalf("after b was gone %v: %s", peerGrace, got)
	}
	if _, ok := seen["b"]; ok {
		t.Error("b's time stays remembered")
	}
}

func TestPullOutcome(t *testing.T) {
	boom := errors.New("boom")
	for _, c := range []struct {
		name        string
		err, ctxErr error
		want        pullState
	}{
		{"done", nil, nil, pullDone},
		{"done as the cancel arrives", nil, context.Canceled, pullDone},
		{"cancelled", context.Canceled, context.Canceled, pullCancelled},
		{"cancel surfacing as another error", io.ErrUnexpectedEOF, context.Canceled, pullCancelled},
		{"failed", boom, nil, pullFailed},
		{"timed out is a failure, not a cancel", context.DeadlineExceeded, context.DeadlineExceeded, pullFailed},
		{"stalled", share.ErrStalled, nil, pullFailed},
	} {
		if got := pullOutcome(c.err, c.ctxErr); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	for _, err := range []error{share.ErrChangedFile, share.ErrStalled} {
		if got := friendly(err); !strings.Contains(got, err.Error()) {
			t.Errorf("friendly(%v) = %q", err, got)
		}
	}
}

// TestSameNameDevices checks that peers sharing a name are all marked, a
// unique name is not, and names are compared without case or the space
// around them.
func TestSameNameDevices(t *testing.T) {
	peers := []share.Peer{
		{Name: "书房的 Mac", Device: "a"},
		{Name: "客厅的 Mac", Device: "b"},
		{Name: " 书房的 Mac ", Device: "c"},
		{Name: "Studio", Device: "d"},
		{Name: "studio", Device: "e"},
		{Name: "卧室", Device: "f"},
	}
	got := sameNameDevices(peers)
	for d, want := range map[string]bool{"a": true, "b": false, "c": true, "d": true, "e": true, "f": false} {
		if got[d] != want {
			t.Errorf("device %s: marked %v, want %v", d, got[d], want)
		}
	}
	if len(sameNameDevices(nil)) != 0 || len(sameNameDevices(peers[:2])) != 0 {
		t.Error("marked peers with distinct names")
	}
}

func TestCodeLines(t *testing.T) {
	first, second := codeLines("ABCD-EFGH-IJKL-MNOP-QRST-UVWX-2345-6723")
	if first != "ABCD-EFGH-IJKL-MNOP" || second != "QRST-UVWX-2345-6723" {
		t.Errorf("codeLines: %q %q", first, second)
	}
	if first, second = codeLines("not a code"); first != "not a code" || second != "" {
		t.Errorf("codeLines of a stranger: %q %q", first, second)
	}
}
