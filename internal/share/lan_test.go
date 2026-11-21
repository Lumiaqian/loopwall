package share

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIsLocalAddr(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "::1": true,
		"10.1.2.3": true, "172.16.0.1": true, "172.31.255.255": true, "192.168.1.20": true,
		"fd12:3456::1": true, "fc00::1": true,
		"169.254.10.1": true, "fe80::1%en0": true, "::ffff:192.168.1.5": true,
		"8.8.8.8": false, "172.32.0.1": false, "11.0.0.1": false, "100.64.0.1": false,
		"2001:4860:4860::8888": false, "fec0::1": false, "::ffff:8.8.8.8": false, "0.0.0.0": false,
	} {
		if got := isLocalAddr(netip.MustParseAddr(ip)); got != want {
			t.Errorf("isLocalAddr(%s) = %v, want %v", ip, got, want)
		}
	}
}

type fakeConn struct {
	net.Conn
	addr   string
	closed bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return fakeAddr(c.addr) }
func (c *fakeConn) Close() error         { c.closed = true; return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

type fakeListener struct {
	net.Listener
	conns []*fakeConn
}

func (l *fakeListener) Accept() (net.Conn, error) {
	if len(l.conns) == 0 {
		return nil, net.ErrClosed
	}
	c := l.conns[0]
	l.conns = l.conns[1:]
	return c, nil
}

func TestLocalOnlyClosesOtherConnections(t *testing.T) {
	outside1, outside2 := &fakeConn{addr: "8.8.8.8:5000"}, &fakeConn{addr: "[2001:db8::1]:5000"}
	inside := &fakeConn{addr: "192.168.1.9:6000"}
	ln := localOnly(&fakeListener{conns: []*fakeConn{outside1, outside2, inside}})
	c, err := ln.Accept()
	if err != nil || c != net.Conn(inside) {
		t.Fatalf("accepted %v, %v", c, err)
	}
	if !outside1.closed || !outside2.closed || inside.closed {
		t.Fatalf("closed: %v %v %v", outside1.closed, outside2.closed, inside.closed)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("no error once the listener is done")
	}
}

// TestServerLimitsRequests checks that the server refuses oversized
// headers and bounds how long it reads a request.
func TestServerLimitsRequests(t *testing.T) {
	root, _ := library(t)
	s, _, peer := serve(t, root)
	if s.srv.ReadTimeout <= 0 || s.srv.MaxHeaderBytes != 16<<10 {
		t.Fatalf("ReadTimeout %v, MaxHeaderBytes %d", s.srv.ReadTimeout, s.srv.MaxHeaderBytes)
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+peer.Addr+"/v1/library", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Pad", strings.Repeat("a", 32<<10))
	resp, err := httpClient(nil, func(string) error { return nil }, nil, 5*time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status %d for 32 KiB of header", resp.StatusCode)
	}
}
