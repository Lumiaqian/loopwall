package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
)

func TestFriendly(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{share.ErrWrongCode, "配对码不对"},
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
