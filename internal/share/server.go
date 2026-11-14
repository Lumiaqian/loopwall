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

	"github.com/Lumiaqian/phosphene-go/internal/catalog"
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

type pairRequest struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Code   string `json:"code"`
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

	mu       sync.Mutex
	code     string
	failures int
	srv      *http.Server
	port     int
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

// Code is the pairing code, empty once too many wrong ones voided it.
func (s *Server) Code() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
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
	srv := &http.Server{
		Handler:           s.Handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Minute,
		// Bonjour's resolving connects and hangs up: no handshake to log.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	s.mu.Lock()
	s.srv, s.port = srv, ln.Addr().(*net.TCPAddr).Port
	s.mu.Unlock()
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	return s.Port(), nil
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
	mux.Handle("GET /v1/library", s.authorized(s.library))
	mux.Handle("GET /v1/thumbnail/{id}", s.authorized(s.media(catalog.ThumbnailPath)))
	mux.Handle("GET /v1/video/{id}", s.authorized(s.media(catalog.VideoPath)))
	return mux
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Device == "" || len(req.Device) > 64 {
		http.Error(w, "bad request", http.StatusBadRequest)
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
	if err := s.store.Grant(req.Device, name, token); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.NewCode() // one code, one pairing
	writeJSON(w, pairResponse{Token: token})
}

// authorized lets through the requests of paired devices only: the
// device's identifier and its token.
func (s *Server) authorized(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		g, ok := s.store.granted(r.Header.Get("X-Phosphene-Device"))
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(hashHex(token)), []byte(g.TokenHash)) != 1 {
			http.Error(w, "not paired", http.StatusUnauthorized)
			return
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
