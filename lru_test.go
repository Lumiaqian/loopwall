package main

import "testing"

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	l := newLRU[string, int](2)
	l.Put("a", 1)
	l.Put("b", 2)
	if v, ok := l.Get("a"); !ok || v != 1 { // a is now newer than b
		t.Fatalf("get a: %v %v", v, ok)
	}
	l.Put("c", 3)
	if l.Has("b") || !l.Has("a") || !l.Has("c") || l.Len() != 2 {
		t.Fatalf("after overflow: a=%v b=%v c=%v len=%d", l.Has("a"), l.Has("b"), l.Has("c"), l.Len())
	}
	l.Put("a", 10) // replaces in place, no growth
	if v, _ := l.Get("a"); v != 10 || l.Len() != 2 {
		t.Fatalf("replace: %d, len %d", v, l.Len())
	}
	l.Remove("a")
	l.Remove("missing")
	if l.Has("a") || l.Len() != 1 {
		t.Fatalf("remove: len %d", l.Len())
	}
	if _, ok := l.Get("a"); ok {
		t.Fatal("got a removed key")
	}
}

func TestLRUHasDoesNotRefresh(t *testing.T) {
	l := newLRU[int, int](2)
	l.Put(1, 1)
	l.Put(2, 2)
	l.Has(1)
	l.Put(3, 3)
	if l.Has(1) {
		t.Fatal("Has counted as a use")
	}
}
