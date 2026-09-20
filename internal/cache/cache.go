// Package cache is the blog's one in-process cache: the home page's
// entry list, the feeds and the pods, dropped wholesale whenever
// anything is written and when an admin asks for `?reinit=1`
// (PLAN §11 "Caching", §9 A29).
//
// BlogCFC kept its cache in the application scope and cleared it with
// `scopecache.cfm clearall="true"` after every save; there was never a
// per-key invalidation and there is none here either. One flush, one
// generation, and whatever asks next fills again.
package cache

import "sync"

// Cache is a keyed memo of whatever a filler returns. It is safe for
// concurrent use, it never expires an entry by time, and every entry is
// stamped with the generation it was filled in: Flush bumps the
// generation, so a value from before the flush is never handed out
// again even if a filler that was already running stores it.
//
// A nil *Cache is a working cache that caches nothing, which is how a
// module with no cache wired in (every test that predates this one)
// keeps behaving exactly as it did.
type Cache struct {
	mu   sync.Mutex
	gen  uint64
	vals map[string]*slot
}

// slot is one key's value, or the promise of one: the first caller to
// ask for a missing key fills it while later callers wait on ready, so
// a cold home page is one query however many readers arrive at once.
type slot struct {
	gen   uint64
	ready chan struct{}
	val   any
	err   error
}

// New returns an empty cache at generation 1.
func New() *Cache {
	return &Cache{gen: 1, vals: map[string]*slot{}}
}

// Get returns the value for key, calling fill when this generation has
// none. A fill that fails is not cached: the error goes to the caller
// and the next request tries again.
func (c *Cache) Get(key string, fill func() (any, error)) (any, error) {
	if c == nil {
		return fill()
	}
	c.mu.Lock()
	if c.vals == nil {
		c.vals = map[string]*slot{}
	}
	if c.gen == 0 {
		c.gen = 1
	}
	gen := c.gen
	if s, ok := c.vals[key]; ok && s.gen == gen {
		c.mu.Unlock()
		<-s.ready
		return s.val, s.err
	}
	s := &slot{gen: gen, ready: make(chan struct{})}
	c.vals[key] = s
	c.mu.Unlock()

	s.val, s.err = fill()
	close(s.ready)

	if s.err != nil {
		c.mu.Lock()
		if cur, ok := c.vals[key]; ok && cur == s {
			delete(c.vals, key)
		}
		c.mu.Unlock()
	}
	return s.val, s.err
}

// Flush drops everything and starts a new generation. The permalink and
// category-alias memo maps the plan speaks of are rebuilt the same way:
// they are keys in here, so they go with the rest (PLAN §11).
func (c *Cache) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.vals = map[string]*slot{}
}

// Generation is how many times the cache has been flushed, plus one. A
// caller that wants to know whether its value is still current compares
// the number it saw with this one.
func (c *Cache) Generation() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// Len is the number of filled keys, for tests and a later stats line.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.vals)
}

// Value is Get with the type assertion done for the caller: the typed
// helper the packages use, so nobody outside this file writes `.(T)`.
// A value of the wrong type is treated as a miss and refilled.
func Value[T any](c *Cache, key string, fill func() (T, error)) (T, error) {
	if c == nil {
		return fill()
	}
	v, err := c.Get(key, func() (any, error) { return fill() })
	if err != nil {
		var zero T
		return zero, err
	}
	t, ok := v.(T)
	if !ok {
		return fill()
	}
	return t, nil
}
