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

// Pair pairs with peer by the code it shows, pinning the certificate it
// presents.
func (c *Client) Pair(ctx context.Context, peer Peer, code string) error {
	var seen string
	hc := httpClient(func(fp string) error { seen = fp; return nil }, 15*time.Second)
	body, _ := json.Marshal(pairRequest{Device: c.store.Device(), Name: c.name, Code: code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+peer.Addr+"/v1/pair", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
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

// Download writes peer's video id to w, reporting how much of it came, of
// how much, as it does. It fails unless all of it came.
func (c *Client) Download(ctx context.Context, peer Peer, id string, w io.Writer, progress func(done, total int64)) error {
	resp, err := c.get(ctx, peer, "/v1/video/"+id, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	n, err := io.Copy(w, &counter{r: resp.Body, total: total, progress: progress})
	if err != nil {
		return err
	}
	if total >= 0 && n != total {
		return fmt.Errorf("下载不完整（%d / %d 字节）：%w", n, total, io.ErrUnexpectedEOF)
	}
	return nil
}

// get makes an authorized request of peer, over its pinned certificate.
// A timeout of 0 lets it take as long as the context does.
func (c *Client) get(ctx context.Context, peer Peer, path string, timeout time.Duration) (*http.Response, error) {
	p, ok := c.store.Paired(peer.Device)
	if !ok {
		return nil, ErrNotPaired
	}
	hc := httpClient(func(fp string) error {
		if fp != p.Fingerprint {
			return ErrChangedCertificate
		}
		return nil
	}, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+peer.Addr+path, nil)
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
	case http.StatusOK:
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
// fingerprint, as the peer's is self-signed.
func httpClient(check func(fingerprint string) error, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives:   true, // a client per request
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				// The chain is not verified: the fingerprint is.
				InsecureSkipVerify: true,
				VerifyConnection: func(cs tls.ConnectionState) error {
					if len(cs.PeerCertificates) == 0 {
						return errors.New("对方没有出示证书")
					}
					return check(Fingerprint(cs.PeerCertificates[0].Raw))
				},
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
