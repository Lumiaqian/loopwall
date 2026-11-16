package main

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestRedrawUntil checks that a preview window keeps redrawing until its
// first picture shows, and no longer; and gives up at its limit.
func TestRedrawUntil(t *testing.T) {
	done := make(chan struct{})
	var n atomic.Int32
	finished := make(chan struct{})
	go func() {
		redrawUntil(done, time.Millisecond, time.Minute, func() { n.Add(1) })
		close(finished)
	}()
	time.Sleep(30 * time.Millisecond)
	if n.Load() < 5 {
		t.Fatalf("redrew %d times in 30ms, every 1ms", n.Load())
	}
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("still redrawing after the picture showed")
	}
	after := n.Load()
	time.Sleep(10 * time.Millisecond)
	if n.Load() != after {
		t.Error("redrew after it returned")
	}

	start := time.Now()
	redrawUntil(make(chan struct{}), time.Millisecond, 20*time.Millisecond, func() {})
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("gave up after %v, not at its 20ms limit", d)
	}
}
