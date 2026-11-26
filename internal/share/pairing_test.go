package share

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder remembers the requests that reach a handler.
type recorder struct {
	mu     sync.Mutex
	bodies []string
	next   http.Handler
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	r.mu.Lock()
	r.bodies = append(r.bodies, req.Method+" "+req.URL.Path+" "+string(b))
	r.mu.Unlock()
	req.Body = io.NopCloser(strings.NewReader(string(b)))
	r.next.ServeHTTP(w, req)
}

// seen is the requests that reached the recorder, one line each.
func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.bodies...)
}

// startTLS serves h over TLS with the certificate of st, as a Mac that is
// not the one the code names, on the loopback; it returns its address.
func startTLS(t *testing.T, st *Store, h http.Handler) string {
	t.Helper()
	cert, err := st.Certificate()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, ClientAuth: tls.RequestClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// untouched reports whether the server's secret is untouched: the same, and
// no wrong guess counted against it.
func untouched(s *Server, secret string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code == secret && s.failures == 0
}

// TestImpersonatorGetsNothing is the attack of P1-3: a Mac that advertises
// the same name, with a certificate of its own, while the user types the
// code of the real one. The handshake is refused, so not a request, and
// with it not the secret nor the reverse token, reaches it.
func TestImpersonatorGetsNothing(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, _, real := serve(t, root)
	attackerStore := open(t)
	rec := &recorder{next: NewServer(root, attackerStore).Handler()}
	addr := startTLS(t, attackerStore, rec)
	client := NewClient(open(t), "客厅的 Mac")

	// The same name and device as the real one: only the address differs.
	fake := Peer{Name: real.Name, Device: real.Device, Addr: addr}
	secret := s.Code()
	err := client.Pair(ctx, fake, s.PairingCode())
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("the impersonator received %q", got)
	}
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("pairing with an impersonator: %v, want ErrFingerprintMismatch", err)
	}
	if _, ok := client.store.granted(real.Device); ok {
		t.Error("the reverse access was granted to an impersonator")
	}
	if _, ok := client.store.Paired(real.Device); ok {
		t.Error("an impersonator was pinned")
	}
	if !untouched(s, secret) {
		t.Error("the real server's secret was touched")
	}
	// The real one still pairs with the same code.
	if err = client.Pair(ctx, real, s.PairingCode()); err != nil {
		t.Fatalf("the real server: %v", err)
	}
}

// TestLegacyPairingCannotDetectImpersonation documents why six digits are
// unsafe: the same impersonator is given the secret.
func TestLegacyPairingCannotDetectImpersonation(t *testing.T) {
	root, _ := library(t)
	s, _, real := serve(t, root)
	attackerStore := open(t)
	rec := &recorder{next: NewServer(root, attackerStore).Handler()}
	addr := startTLS(t, attackerStore, rec)
	client := NewClient(open(t), "客厅的 Mac")
	_ = client.Pair(context.Background(), Peer{Name: real.Name, Device: real.Device, Addr: addr}, s.Code())
	got := rec.seen()
	if len(got) != 1 || !strings.Contains(got[0], s.Code()) {
		t.Fatalf("the impersonator was expected to receive the legacy code, got %q", got)
	}
}

// TestRelayIsRefused is a man in the middle: a relay that terminates TLS
// with a certificate of its own and forwards to the real server. The
// client refuses it in the handshake, before a request is forwarded.
func TestRelayIsRefused(t *testing.T) {
	root, _ := library(t)
	s, _, real := serve(t, root)
	target, _ := url.Parse("https://" + real.Addr)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	rec := &recorder{next: proxy}
	addr := startTLS(t, open(t), rec)
	client := NewClient(open(t), "客厅的 Mac")

	secret := s.Code()
	err := client.Pair(context.Background(), Peer{Name: real.Name, Device: real.Device, Addr: addr}, s.PairingCode())
	if got := rec.seen(); len(got) != 0 {
		t.Fatalf("the relay forwarded %q", got)
	}
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("pairing through a relay: %v, want ErrFingerprintMismatch", err)
	}
	if !untouched(s, secret) {
		t.Error("the real server saw a guess")
	}
}

// acceptCounter counts the connections its listener accepts.
type acceptCounter struct {
	net.Listener
	mu sync.Mutex
	n  int
}

func (l *acceptCounter) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.n++
		l.mu.Unlock()
	}
	return c, err
}

func (l *acceptCounter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

// TestMistypedCodeIsCaughtBeforeDialing checks a bad check character is
// reported without a connection; and that a code whose check characters
// are right but whose fingerprint is not fails at the handshake.
func TestMistypedCodeIsCaughtBeforeDialing(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, _, real := serve(t, root)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	counter := &acceptCounter{Listener: ln}
	srv := &http.Server{Handler: s.Handler()}
	cert, _ := s.store.Certificate()
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	go func() { _ = srv.ServeTLS(counter, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	peer := Peer{Name: real.Name, Device: real.Device, Addr: ln.Addr().String()}
	client := NewClient(open(t), "x")

	code := []byte(s.PairingCode())
	code[2] = 'A' + (code[2]-'A'+1)%26
	if err = client.Pair(ctx, peer, string(code)); !errors.Is(err, ErrCodeTypo) {
		t.Fatalf("a mistyped code: %v, want ErrCodeTypo", err)
	}
	if err = client.Pair(ctx, peer, "ABCD-EFGH"); !errors.Is(err, ErrCodeFormat) {
		t.Fatalf("a short code: %v, want ErrCodeFormat", err)
	}
	if n := counter.count(); n != 0 {
		t.Fatalf("%d connections were made for codes that are not codes", n)
	}

	// Valid check characters, another certificate's fingerprint.
	other, _ := open(t).CertFingerprint()
	forged, err := EncodePairCode(other, s.Code())
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Pair(ctx, peer, forged); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("a code of another certificate: %v, want ErrFingerprintMismatch", err)
	}
	if !untouched(s, s.Code()) {
		t.Error("the secret was touched")
	}
	if _, ok := client.store.granted(peer.Device); ok {
		t.Error("granted to a peer that failed the handshake")
	}
}

// post sends a pairing request as a client that presents cert, if any.
func post(t *testing.T, addr string, cert *tls.Certificate, body string) int {
	t.Helper()
	hc := httpClient(cert, func(string) error { return nil }, nil, 5*time.Second)
	resp, err := hc.Post("https://"+addr+"/v1/pair", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestPairRequestIsBoundToItsCertificate checks that a version 2 request
// is refused, before its secret is tried, unless its reverse access names
// the certificate it presents.
func TestPairRequestIsBoundToItsCertificate(t *testing.T) {
	root, _ := library(t)
	s, served, peer := serve(t, root)
	mine, other := open(t), open(t)
	myCert, _ := mine.Certificate()
	myFP, _ := mine.CertFingerprint()
	otherFP, _ := other.CertFingerprint()
	token := randomHex(32)
	req := func(fp string) string {
		return `{"version":2,"device":"` + mine.Device() + `","name":"x","code":"` + s.Code() + `","back":{"token":"` + token + `","fingerprint":"` + fp + `"}}`
	}
	secret := s.Code()

	if got := post(t, peer.Addr, &myCert, req(otherFP)); got != http.StatusBadRequest {
		t.Errorf("a back fingerprint of another certificate: %d, want 400", got)
	}
	if got := post(t, peer.Addr, nil, req(myFP)); got != http.StatusBadRequest {
		t.Errorf("no client certificate: %d, want 400", got)
	}
	if got := post(t, peer.Addr, &myCert, `{"version":2,"device":"d","code":"`+s.Code()+`"}`); got != http.StatusBadRequest {
		t.Errorf("no reverse access: %d, want 400", got)
	}
	if got := post(t, peer.Addr, &myCert, `{"version":3,"device":"d","code":"`+s.Code()+`"}`); got != http.StatusBadRequest {
		t.Errorf("an unknown version: %d, want 400", got)
	}
	if !untouched(s, secret) {
		t.Error("a malformed request used up the secret")
	}
	if len(served.Granted()) != 0 {
		t.Error("a malformed request was granted")
	}
	if got := post(t, peer.Addr, &myCert, req(myFP)); got != http.StatusOK {
		t.Fatalf("a well-formed request: %d", got)
	}
	if g := served.Granted()[mine.Device()]; g.ClientFingerprint != myFP {
		t.Errorf("the grant is bound to %q, want %q", g.ClientFingerprint, myFP)
	}
	if p, _ := served.Paired(mine.Device()); p.Fingerprint != myFP {
		t.Errorf("the reverse access pins %q, want %q", p.Fingerprint, myFP)
	}
}

// TestTokenAloneIsNotEnough checks that a v2 grant wants the certificate it
// was made to, as well as the token.
func TestTokenAloneIsNotEnough(t *testing.T) {
	ctx := context.Background()
	root, _ := library(t)
	s, served, peer := serve(t, root)
	client := NewClient(open(t), "客厅的 Mac")
	if err := client.Pair(ctx, peer, s.PairingCode()); err != nil {
		t.Fatal(err)
	}
	p, _ := client.store.Paired(peer.Device)
	myCert, _ := client.store.Certificate()
	otherCert, _ := open(t).Certificate()
	get := func(cert *tls.Certificate) int {
		hc := httpClient(cert, func(string) error { return nil }, nil, 5*time.Second)
		req, _ := http.NewRequest(http.MethodGet, "https://"+peer.Addr+"/v1/library", nil)
		req.Header.Set("Authorization", "Bearer "+p.Token)
		req.Header.Set("X-Loopwall-Device", client.store.Device())
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(&myCert); got != http.StatusOK {
		t.Errorf("the certificate it paired with: %d", got)
	}
	if got := get(&otherCert); got != http.StatusUnauthorized {
		t.Errorf("the token with another certificate: %d, want 401", got)
	}
	if got := get(nil); got != http.StatusUnauthorized {
		t.Errorf("the token with no certificate: %d, want 401", got)
	}
	// The reverse grant of the typing Mac is bound to the server's.
	fp, _ := served.CertFingerprint()
	if g, ok := client.store.granted(peer.Device); !ok || g.ClientFingerprint != fp {
		t.Errorf("the grant to the server: %+v, want it bound to %s", g, fp)
	}
}

func TestConcurrentCodeConsumption(t *testing.T) {
	root, _ := library(t)
	server, _, peer := serve(t, root)
	code := server.PairingCode()
	start := make(chan struct{})
	const attempts = 4
	results := make(chan error, attempts)
	for range attempts {
		client := NewClient(open(t), "test")
		go func() {
			<-start
			results <- client.Pair(context.Background(), peer, code)
		}()
	}
	close(start)
	successes := 0
	for range attempts {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrWrongCode) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("one code authorized %d clients", successes)
	}
}
