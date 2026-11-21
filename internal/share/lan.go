package share

import (
	"net"
	"net/netip"
)

// isLocalAddr reports whether ip is one a Mac on the same network
// connects from: loopback, private (RFC 1918, IPv6 ULA fc00::/7) or
// link-local (169.254.0.0/16, fe80::/10). Bonjour peers are on the local
// link, so a connection from anywhere else is not one of them.
func isLocalAddr(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// localOnly wraps ln to close, unserved, the connections that do not come
// from a local address.
func localOnly(ln net.Listener) net.Listener { return &localListener{ln} }

type localListener struct{ net.Listener }

func (l *localListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if ap, perr := netip.ParseAddrPort(c.RemoteAddr().String()); perr == nil && isLocalAddr(ap.Addr()) {
			return c, nil
		}
		_ = c.Close()
	}
}
