package share

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

// maxFailures is how many wrong pairing codes invalidate the code: a
// guess in a million, five times.
const maxFailures = 5

// Item is a video a server shares.
type Item struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Ext             string  `json:"ext"`
	Width           int64   `json:"width"`
	Height          int64   `json:"height"`
	DurationSeconds float64 `json:"durationSeconds"`
	FrameRate       float64 `json:"frameRate"`
	Size            int64   `json:"size"`
}

// pairVersion is the version of the pairing request that binds it to
// certificates. A request that names none is of versions before it, whose
// code is six digits and whose peer is not told from another.
const pairVersion = 2

// errHeader names, on a refusal, why, where the status alone cannot:
// versions before 2 know a 403 as a wrong code and nothing else.
const (
	errHeader       = "X-Loopwall-Error"
	errLegacyDenied = "legacy-disabled"
)

type pairRequest struct {
	// Version is pairVersion for a request that presents a client
	// certificate: the one whose fingerprint Back names.
	Version int    `json:"version,omitempty"`
	Device  string `json:"device"`
	Name    string `json:"name"`
	// Code is the one-time secret, six digits.
	Code string `json:"code"`
	// Back is the access the pairing device grants in return, so that one
	// code pairs both ways. Versions before 0.1.3 send none.
	Back *backAccess `json:"back,omitempty"`
}

// backAccess is a token the pairing device accepts from this Mac, and the
// fingerprint of the certificate its own server presents.
type backAccess struct {
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
}

// hex64 reports whether s is 32 bytes in hex, as tokens and fingerprints are.
func hex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

type pairResponse struct {
	Token string `json:"token"`
}

// Server shares the library at root, read-only, with the devices paired
// with it: its videos, their thumbnails and its list. A device pairs with
// the code the server shows, which turns over after each pairing and goes
// void after maxFailures wrong ones.
type Server struct {
	root  string
	store *Store
	// failDelay slows each wrong code down.
	failDelay time.Duration

	mu          sync.Mutex
	code        string
	failures    int
	allowLegacy bool
	srv         *http.Server
	port        int
}

// NewServer makes a server sharing the library at root.
func NewServer(root string, store *Store) *Server {
	s := &Server{root: root, store: store, failDelay: 500 * time.Millisecond}
	s.NewCode()
	return s
}

// NewCode turns the pairing code over.
func (s *Server) NewCode() {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	s.code, s.failures = fmt.Sprintf("%06d", n.Int64()), 0
	s.mu.Unlock()
}

// Code is the one-time secret, six digits, empty once too many wrong ones
// voided it: what a legacy code is, and what the long one ends in.
func (s *Server) Code() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// PairingCode is the code to show, version 2: this server's certificate
// and the secret, as ParsePairCode reads. Empty once the secret is void.
func (s *Server) PairingCode() string {
	secret := s.Code()
	if secret == "" {
		return ""
	}
	fp, err := s.store.CertFingerprint()
	if err != nil {
		return ""
	}
	code, err := EncodePairCode(fp, secret)
	if err != nil {
		return ""
	}
	return code
}

// Start serves on addr, ":0" for any port, and returns the port.
func (s *Server) Start(addr string) (int, error) {
	cert, err := s.store.Certificate()
	if err != nil {
		return 0, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	ln = localOnly(ln)
	srv := &http.Server{
		Handler: s.Handler(),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
			// Asked for, not required: a client before version 2 sends
			// none. The chain is not verified, the fingerprint is.
			ClientAuth: tls.RequestClientCert,
		},
		ReadHeaderTimeout: 10 * time.Second,
		// Requests are small: a slow connection cannot hold a slot.
		ReadTimeout:    30 * time.Second,
		MaxHeaderBytes: 16 << 10,
		IdleTimeout:    time.Minute,
		// Bonjour's resolving connects and hangs up: no handshake to log.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	s.mu.Lock()
	s.srv, s.port = srv, ln.Addr().(*net.TCPAddr).Port
	s.mu.Unlock()
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	return s.Port(), nil
}

// SetLegacy turns on or off accepting pairing requests of versions before
// 2, which carry six digits and no certificate, and so cannot tell this
// Mac from one that impersonates it. Off at first.
func (s *Server) SetLegacy(on bool) {
	s.mu.Lock()
	s.allowLegacy = on
	s.mu.Unlock()
}

// Legacy reports whether SetLegacy is on.
func (s *Server) Legacy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowLegacy
}

// Port is the port the server listens on, 0 when stopped.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Stop closes the server and the connections it serves.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.port = nil, 0
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

// Handler is the server's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.Handle("POST /v1/unpair", s.authorized(s.unpair))
	mux.Handle("GET /v1/library", s.authorized(s.library))
	mux.Handle("GET /v1/thumbnail/{id}", s.authorized(s.media(catalog.ThumbnailPath)))
	mux.Handle("GET /v1/video/{id}", s.authorized(s.media(catalog.VideoPath)))
	return mux
}

// peerFingerprint is the fingerprint of the certificate the client of r
// presented in the handshake, which proved it holds its key.
func peerFingerprint(r *http.Request) (string, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	return Fingerprint(r.TLS.PeerCertificates[0].Raw), true
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Device == "" || len(req.Device) > 64 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// What the request must be, before the secret is looked at: a
	// request that is not well formed is no guess at it.
	var bound string // the fingerprint of the client's certificate
	switch {
	case req.Version > pairVersion:
		http.Error(w, "unsupported version", http.StatusBadRequest)
		return
	case req.Version == pairVersion:
		fp, ok := peerFingerprint(r)
		if b := req.Back; !ok || b == nil || !hex64(b.Token) || b.Fingerprint != fp {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		bound = fp
	case !s.Legacy() || s.verifiedDevice(req.Device):
		// Refused as what it is, not as a guess: the secret is not
		// looked at, so this is no way to try one. A device paired by the
		// long code is never re-paired the legacy way, which would replace
		// its bound grant and pinned certificate on the strength of six
		// digits.
		w.Header().Set(errHeader, errLegacyDenied)
		http.Error(w, "legacy pairing is off", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	ok := s.code != "" && subtle.ConstantTimeCompare([]byte(req.Code), []byte(s.code)) == 1
	if !ok && s.code != "" {
		if s.failures++; s.failures >= maxFailures {
			s.code = ""
		}
	}
	s.mu.Unlock()
	if !ok {
		time.Sleep(s.failDelay)
		http.Error(w, "wrong code", http.StatusForbidden)
		return
	}
	token := randomHex(32)
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 {
		name = "Mac"
	}
	if err := s.store.Grant(req.Device, name, token, bound); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The code proved the device is the user's: what it grants in return,
	// sent over the same connection, pairs this Mac with it too.
	if b := req.Back; b != nil && hex64(b.Token) && hex64(b.Fingerprint) {
		if err := s.store.Pair(req.Device, Paired{Name: name, Token: b.Token, Fingerprint: b.Fingerprint, Verified: bound != ""}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.NewCode() // one code, one pairing
	writeJSON(w, pairResponse{Token: token})
}

// unpair forgets the device that asks, both ways: it may pull from this
// Mac no longer, nor this Mac from it.
func (s *Server) unpair(w http.ResponseWriter, r *http.Request) {
	device := r.Header.Get("X-Loopwall-Device")
	err := s.store.Revoke(device)
	if err == nil {
		err = s.store.Unpair(device)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authorized lets through the requests of paired devices only: the
// device's identifier and its token, and for a pairing of version 2 the
// certificate it paired with, presented in this connection's handshake.
func (s *Server) authorized(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		g, ok := s.store.granted(r.Header.Get("X-Loopwall-Device"))
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(hashHex(token)), []byte(g.TokenHash)) != 1 {
			http.Error(w, "not paired", http.StatusUnauthorized)
			return
		}
		if g.ClientFingerprint != "" {
			if fp, ok := peerFingerprint(r); !ok || subtle.ConstantTimeCompare([]byte(fp), []byte(g.ClientFingerprint)) != 1 {
				http.Error(w, "not paired", http.StatusUnauthorized)
				return
			}
		}
		h(w, r)
	})
}

func (s *Server) library(w http.ResponseWriter, _ *http.Request) {
	c, err := catalog.Load(s.root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]Item, 0, len(c.Media))
	for _, m := range c.Media {
		info, err := os.Stat(catalog.VideoPath(s.root, m))
		if err != nil {
			continue // not on disk: nothing to pull
		}
		items = append(items, Item{ID: m.ID, Name: m.Name, Ext: filepath.Ext(m.Filename), Width: m.Width, Height: m.Height,
			DurationSeconds: m.DurationSeconds, FrameRate: m.FrameRate, Size: info.Size()})
	}
	writeJSON(w, items)
}

// media serves the file path names of the library's video {id}: only
// those of videos in the library, so no path is the request's.
func (s *Server) media(path func(string, catalog.MediaEntry) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := catalog.Load(s.root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id := r.PathValue("id")
		for _, m := range c.Media {
			if m.ID != id {
				continue
			}
			f, err := os.Open(path(s.root, m))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.ServeContent(w, r, filepath.Base(f.Name()), info.ModTime(), f)
			return
		}
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// verifiedDevice reports whether device was paired by the long code, either
// way: its grant is bound to its certificate, or its pin is verified.
func (s *Server) verifiedDevice(device string) bool {
	if g, ok := s.store.granted(device); ok && g.ClientFingerprint != "" {
		return true
	}
	p, ok := s.store.Paired(device)
	return ok && p.Verified
}
