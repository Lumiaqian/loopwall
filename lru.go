package main

import "container/list"

// lru is a cache of at most max entries that drops the least recently used
// one when it is full. It is not safe for concurrent use: the app's caches
// are touched on the main thread only.
type lru[K comparable, V any] struct {
	max   int
	order *list.List // front is the most recently used
	items map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key K
	val V
}

func newLRU[K comparable, V any](max int) *lru[K, V] {
	return &lru[K, V]{max: max, order: list.New(), items: map[K]*list.Element{}}
}

// Get returns the value of key and counts it as used.
func (l *lru[K, V]) Get(key K) (V, bool) {
	e, ok := l.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	l.order.MoveToFront(e)
	return e.Value.(*lruEntry[K, V]).val, true
}

// Has reports whether key is cached, without counting it as used.
func (l *lru[K, V]) Has(key K) bool {
	_, ok := l.items[key]
	return ok
}

// Put stores val under key, dropping the least recently used entry when
// that goes over the limit.
func (l *lru[K, V]) Put(key K, val V) {
	if e, ok := l.items[key]; ok {
		e.Value.(*lruEntry[K, V]).val = val
		l.order.MoveToFront(e)
		return
	}
	l.items[key] = l.order.PushFront(&lruEntry[K, V]{key, val})
	for l.order.Len() > l.max {
		oldest := l.order.Back()
		l.order.Remove(oldest)
		delete(l.items, oldest.Value.(*lruEntry[K, V]).key)
	}
}

// Remove drops key.
func (l *lru[K, V]) Remove(key K) {
	if e, ok := l.items[key]; ok {
		l.order.Remove(e)
		delete(l.items, key)
	}
}

// Len is the number of entries.
func (l *lru[K, V]) Len() int { return l.order.Len() }
