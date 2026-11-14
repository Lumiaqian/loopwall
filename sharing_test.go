package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/Lumiaqian/phosphene-go/internal/share"
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
