package share

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakePeer serves h over TLS and returns a client paired with it.
func fakePeer(t *testing.T, h http.HandlerFunc) (*Client, Peer) {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = false
	srv.StartTLS()
	t.Cleanup(srv.Close)
	st := open(t)
	if err := st.Pair("dev", Paired{Name: "peer", Token: "t", Fingerprint: Fingerprint(srv.TLS.Certificates[0].Certificate[0])}); err != nil {
		t.Fatal(err)
	}
	return NewClient(st, "me"), Peer{Name: "peer", Device: "dev", Addr: srv.Listener.Addr().String()}
}

func TestDownloadHoldsToTheListedSize(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 1000)
	for _, c := range []struct {
		name    string
		size    int64
		serve   http.HandlerFunc
		wantErr error
	}{
		{"exact", 1000, func(w http.ResponseWriter, r *http.Request) { w.Write(body) }, nil},
		{"longer than listed", 500, func(w http.ResponseWriter, r *http.Request) { w.Write(body) }, ErrChangedFile},
		{"shorter than listed", 2000, func(w http.ResponseWriter, r *http.Request) { w.Write(body) }, ErrChangedFile},
		{"unknown length, longer", 500, func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush() // chunked: no Content-Length
			w.Write(body)
		}, ErrChangedFile},
		{"unknown length, shorter", 2000, func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			w.Write(body)
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, peer := fakePeer(t, c.serve)
			var got bytes.Buffer
			err := client.Download(context.Background(), peer, "v", c.size, &got, nil)
			switch {
			case c.name == "unknown length, shorter":
				if err == nil {
					t.Fatal("a short body was accepted")
				}
			case !errors.Is(err, c.wantErr):
				t.Fatalf("err %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil && int64(got.Len()) > c.size {
				t.Fatalf("wrote %d bytes past the size %d", got.Len(), c.size)
			}
		})
	}
}

func TestDownloadStallsOut(t *testing.T) {
	real := downloadIdle
	downloadIdle = 150 * time.Millisecond
	t.Cleanup(func() { downloadIdle = real })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	client, peer := fakePeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	var got bytes.Buffer
	start := time.Now()
	err := client.Download(context.Background(), peer, "v", 1000, &got, nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err %v, want ErrStalled", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the stall was not cut short")
	}
}
