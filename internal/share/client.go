package share

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

var (
	// ErrNotPaired is a peer this Mac has no pairing with, or one that
	// forgot it: pair again.
	ErrNotPaired = errors.New("尚未配对，或对方已取消配对")
	// ErrWrongCode is a pairing code the peer refused; after a few, it
	// voids its code and shows a new one.
	ErrWrongCode = errors.New("配对码不对，或已失效；请看对方 Mac 上显示的配对码")
	// ErrChangedCertificate is a peer presenting another certificate than
	// it paired with: another Mac in its place, or its sharing reset.
	ErrChangedCertificate = errors.New("对方的身份变了，请重新配对")
	// ErrFingerprintMismatch is a peer whose certificate is not the one the
	// pairing code names: nothing was sent to it.
	ErrFingerprintMismatch = errors.New("对方 Mac 的身份与配对码不符，可能有人冒充，已中止配对")
	// ErrChangedFile is a video that is not the size its library listed:
	// the peer changed it since.
	ErrChangedFile = errors.New("对方的文件已变化，请刷新后重试")
	// ErrStalled is a download that received nothing for too long: the peer
	// may have gone to sleep.
	ErrStalled = errors.New("对方长时间没有发来数据，下载已中断")
)

// Peer is a Mac sharing its library, as Bonjour found it.
type Peer struct {
	Name   string
	Device string
	Addr   string // host:port
}

// Client pulls from the Macs this one pairs with, as the device named name.
type Client struct {
	store *Store
	name  string
}

// NewClient makes a client pairing as name.
func NewClient(store *Store, name string) *Client { return &Client{store: store, name: name} }

// Pair pairs with peer by the code it shows, and grants it access in
// return, so that it pulls from this Mac too without a code of this one: a
// peer before 0.1.3 ignores that.
//
// A long code names the certificate of the Mac that shows it: the
// handshake fails, before anything is sent, with ErrFingerprintMismatch
// unless the peer presents that. This Mac presents its own certificate
// too, so the peer can bind what it grants to it. Six digits are the
// legacy code, of versions before 0.2: they pin whatever certificate
// answers, and cannot tell an impersonator.
func (c *Client) Pair(ctx context.Context, peer Peer, code string) error {
	if IsLegacyCode(code) {
		return c.pair(ctx, peer, LegacySecret(code), nil)
	}
	pc, err := ParsePairCode(code) // a mistyped code goes nowhere
	if err != nil {
		return err
	}
	return c.pair(ctx, peer, pc.Secret, &pc)
}

// pair pairs with peer by secret. want, the long code, is nil for the
// legacy flow.
func (c *Client) pair(ctx context.Context, peer Peer, secret string, want *PairCode) (err error) {
	cert, err := c.store.Certificate()
	if err != nil {
		return err
	}
	back := randomHex(32)
	prev, had := c.store.granted(peer.Device)
	granted := false
	defer func() {
		if err == nil || !granted {
			return
		}
		// Taken back, as it was, as the pairing failed.
		if had {
			_ = c.store.restoreGrant(peer.Device, prev)
		} else {
			_ = c.store.Revoke(peer.Device)
		}
	}()
	var seen string
	check := func(fp string) error {
		if want != nil && !want.Matches(fp) {
			return ErrFingerprintMismatch
		}
		return nil
	}
	// Granted once the peer is who the code says, and before it is asked,
	// so that it may list this Mac at once; with a long code, to that
	// certificate alone.
	authenticated := func(fp string) error {
		seen = fp
		bound := ""
		if want != nil {
			bound = fp
		}
		granted = true
		return c.store.Grant(peer.Device, peer.Name, back, bound)
	}
	req := pairRequest{Device: c.store.Device(), Name: c.name, Code: secret,
		Back: &backAccess{Token: back, Fingerprint: Fingerprint(cert.Certificate[0])}}
	var clientCert *tls.Certificate
	if want != nil {
		req.Version, clientCert = pairVersion, &cert
	}
	hc := httpClient(clientCert, check, authenticated, 15*time.Second)
	body, _ := json.Marshal(req)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+peer.Addr+"/v1/pair", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := hc.Do(hreq)
	if err != nil {
		if errors.Is(err, ErrFingerprintMismatch) {
			return ErrFingerprintMismatch
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return ErrWrongCode
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("配对失败：%s", resp.Status)
	}
	var pr pairResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&pr); err != nil || pr.Token == "" || seen == "" {
		return errors.New("配对失败：对方的回应无效")
	}
	return c.store.Pair(peer.Device, Paired{Name: peer.Name, Token: pr.Token, Fingerprint: seen})
}

// Unpair forgets device both ways: it tells the peer at addr, when it is
// found, to forget this Mac too, and forgets it here whatever the peer
// answers, as one that is gone answers nothing.
func (c *Client) Unpair(ctx context.Context, device, addr string) error {
	var told error
	if addr != "" {
		var resp *http.Response
		resp, told = c.request(ctx, http.MethodPost, Peer{Device: device, Addr: addr}, "/v1/unpair", 10*time.Second)
		if told == nil {
			resp.Body.Close()
		}
	}
	if err := c.store.Revoke(device); err != nil {
		return err
	}
	if err := c.store.Unpair(device); err != nil {
		return err
	}
	if errors.Is(told, ErrNotPaired) {
		return nil // forgotten there already
	}
	return told
}

// Library lists the videos peer shares.
func (c *Client) Library(ctx context.Context, peer Peer) ([]Item, error) {
	resp, err := c.get(ctx, peer, "/v1/library", 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var items []Item
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}

// Thumbnail returns the JPEG thumbnail of peer's video id.
func (c *Client) Thumbnail(ctx context.Context, peer Peer, id string) ([]byte, error) {
	resp, err := c.get(ctx, peer, "/v1/thumbnail/"+id, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// downloadIdle is how long a download waits for the next bytes before it
// gives up; tests shorten it.
var downloadIdle = 60 * time.Second

// Download writes peer's video id to w, reporting how much of it came, of
// how much, as it does. size is what the library listed: it fails unless
// exactly that came, so a file that changed since, or a peer sending more
// than it said, does not fill the disk. It also fails when no bytes arrive
// for downloadIdle.
func (c *Client) Download(ctx context.Context, peer Peer, id string, size int64, w io.Writer, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := time.AfterFunc(downloadIdle, func() { cancel(ErrStalled) })
	defer idle.Stop()
	// A stall surfaces as the context's cancellation: say what it was.
	stalled := func(err error) error {
		if errors.Is(context.Cause(ctx), ErrStalled) {
			return ErrStalled
		}
		return err
	}

	resp, err := c.get(ctx, peer, "/v1/video/"+id, 0)
	if err != nil {
		return stalled(err)
	}
	defer resp.Body.Close()
	if resp.ContentLength >= 0 && resp.ContentLength != size {
		return ErrChangedFile
	}
	body := &idleReader{r: resp.Body, timer: idle}
	n, err := io.Copy(w, &counter{r: io.LimitReader(body, size), total: size, progress: progress})
	if err != nil {
		return stalled(err)
	}
	if n < size {
		return fmt.Errorf("下载不完整（%d / %d 字节）：%w", n, size, io.ErrUnexpectedEOF)
	}
	// What is left past size, a peer sending more than it said, is not
	// written: any byte at all is a changed file.
	var extra [1]byte
	if m, _ := body.Read(extra[:]); m > 0 {
		return ErrChangedFile
	}
	return nil
}

// idleReader restarts timer whenever bytes arrive.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(downloadIdle)
	}
	return n, err
}

// get makes an authorized request of peer, over its pinned certificate.
// A timeout of 0 lets it take as long as the context does.
func (c *Client) get(ctx context.Context, peer Peer, path string, timeout time.Duration) (*http.Response, error) {
	return c.request(ctx, http.MethodGet, peer, path, timeout)
}

// request makes an authorized request of peer, as get.
func (c *Client) request(ctx context.Context, method string, peer Peer, path string, timeout time.Duration) (*http.Response, error) {
	p, ok := c.store.Paired(peer.Device)
	if !ok {
		return nil, ErrNotPaired
	}
	cert, err := c.store.Certificate()
	if err != nil {
		return nil, err
	}
	// The certificate is presented too: a pairing of version 2 binds the
	// token to it.
	hc := httpClient(&cert, func(fp string) error {
		if fp != p.Fingerprint {
			return ErrChangedCertificate
		}
		return nil
	}, nil, timeout)
	req, err := http.NewRequestWithContext(ctx, method, "https://"+peer.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("X-Loopwall-Device", c.store.Device())
	resp, err := hc.Do(req)
	if err != nil {
		if errors.Is(err, ErrChangedCertificate) {
			return nil, ErrChangedCertificate
		}
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return resp, nil
	case http.StatusUnauthorized:
		resp.Body.Close()
		return nil, ErrNotPaired
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("对方返回 %s", resp.Status)
	}
}

// httpClient trusts the certificate that check accepts by its
// fingerprint, as the peer's is self-signed, and presents cert, if any, as
// its own. after, if any, is called once the handshake is complete, the
// peer proven to hold the certificate's key, before a request is sent: a
// check in VerifyConnection runs before that proof, so it only turns away.
func httpClient(cert *tls.Certificate, check, after func(fingerprint string) error, timeout time.Duration) *http.Client {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		// The chain is not verified: the fingerprint is.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("对方没有出示证书")
			}
			return check(Fingerprint(cs.PeerCertificates[0].Raw))
		},
	}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: cfg}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true, // a client per request
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second) // dial and handshake
				defer cancel()
				conn, err := dialer.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				if after != nil {
					if err = after(Fingerprint(conn.(*tls.Conn).ConnectionState().PeerCertificates[0].Raw)); err != nil {
						_ = conn.Close()
						return nil, err
					}
				}
				return conn, nil
			},
		},
	}
}

// counter reports how much was read through it.
type counter struct {
	r        io.Reader
	done     int64
	total    int64
	progress func(done, total int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if c.progress != nil && n > 0 {
		c.progress(c.done, c.total)
	}
	return n, err
}

// Addr joins a host and a port.
func Addr(host string, port int64) string { return net.JoinHostPort(host, strconv.FormatInt(port, 10)) }
