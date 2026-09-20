package cache_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/cache"
)

// TestFP_A29_CacheGetFlushAndGeneration covers the cache itself (PLAN
// §9 A29, §11 "Caching"): a key is filled once, Flush drops everything
// and bumps the generation, a failed fill is not kept, and a nil cache
// is a cache that never caches.
func TestFP_A29_CacheGetFlushAndGeneration(t *testing.T) {
	c := cache.New()
	var fills int
	get := func() string {
		v, err := c.Get("home", func() (any, error) {
			fills++
			return "page", nil
		})
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("Get returned %T, want string", v)
		}
		return s
	}

	if got := get(); got != "page" {
		t.Fatalf("Get = %q, want %q", got, "page")
	}
	if got := get(); got != "page" || fills != 1 {
		t.Errorf("a second Get filled again (%d fills)", fills)
	}
	gen := c.Generation()

	c.Flush()
	if c.Generation() != gen+1 {
		t.Errorf("Generation = %d after a flush, want %d", c.Generation(), gen+1)
	}
	if c.Len() != 0 {
		t.Errorf("the cache kept %d keys over a flush", c.Len())
	}
	if get(); fills != 2 {
		t.Errorf("the key was not refilled after a flush (%d fills)", fills)
	}

	// A fill that fails is the caller's error and is not cached.
	boom := errors.New("boom")
	var tries int
	for range 2 {
		if _, err := c.Get("bad", func() (any, error) { tries++; return nil, boom }); !errors.Is(err, boom) {
			t.Fatalf("Get(bad) = %v, want the filler's error", err)
		}
	}
	if tries != 2 {
		t.Errorf("a failed fill was cached (%d tries, want 2)", tries)
	}

	// A nil cache works and caches nothing: a module with no cache wired
	// in behaves exactly as it did before there was one.
	var none *cache.Cache
	nilFills := 0
	for range 2 {
		if _, err := none.Get("home", func() (any, error) { nilFills++; return "page", nil }); err != nil {
			t.Fatalf("nil Get: %v", err)
		}
	}
	if nilFills != 2 {
		t.Errorf("the nil cache cached: %d fills, want 2", nilFills)
	}
	none.Flush() // must not panic
}

// TestFP_A29_CacheIsSafeForConcurrentUse: many readers, one filler per
// key per generation, flushes in the middle.
func TestFP_A29_CacheIsSafeForConcurrentUse(t *testing.T) {
	c := cache.New()
	var fills atomic.Int64
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := cache.Value(c, "k", func() (int, error) {
				fills.Add(1)
				return 42, nil
			})
			if err != nil || v != 42 {
				t.Errorf("Value = %v, %v, want 42", v, err)
			}
			if i%10 == 0 {
				c.Flush()
			}
		}(i)
	}
	wg.Wait()
	if n := fills.Load(); n < 1 || n > 50 {
		t.Errorf("the filler ran %d times, want between 1 and 50", n)
	}
}
