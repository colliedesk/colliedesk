package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestFullLimiterEvictsTheOldestKeyInsteadOfRefusingNewOnes(t *testing.T) {
	limiter := newWebFormLimiter(2, 3)
	start := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	for i, key := range []string{"a", "b", "c"} {
		if ok, _ := limiter.allow(key, start.Add(time.Duration(i)*time.Second)); !ok {
			t.Fatalf("key %s refused while the limiter had room", key)
		}
	}
	// All three keys are live (inside the window) and the map is full: a new key must still get in.
	if ok, _ := limiter.allow("d", start.Add(10*time.Second)); !ok {
		t.Fatal("a new key was refused because the map was full of live keys")
	}
	if got := len(limiter.index); got > 3 {
		t.Fatalf("limiter holds %d keys, want at most 3", got)
	}
	if _, kept := limiter.index["a"]; kept {
		t.Fatal("the oldest key was kept; it should have been evicted")
	}
	// The newest keys keep their counts.
	if _, kept := limiter.index["c"]; !kept {
		t.Fatal("a newer key was evicted")
	}
}

func TestEvictionIsLeastRecentlyUsed(t *testing.T) {
	limiter := newWebFormLimiter(5, 3)
	start := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	for i, key := range []string{"a", "b", "c"} {
		limiter.allow(key, start.Add(time.Duration(i)*time.Second))
	}
	limiter.allow("a", start.Add(5*time.Second)) // a is now the most recently used
	limiter.allow("d", start.Add(6*time.Second)) // evicts b, the least recently used
	for key, want := range map[string]bool{"a": true, "b": false, "c": true, "d": true} {
		if _, kept := limiter.index[key]; kept != want {
			t.Errorf("key %s kept=%v, want %v", key, kept, want)
		}
	}
}

// A new key at full capacity must cost the same as at any other size: one address with a whole IPv6
// /48 can make 65536 keys, and every request of every workspace waits on the limiter's lock.
func TestInsertsAtFullCapacityDoNotScanTheMap(t *testing.T) {
	const keys = 20000
	limiter := newWebFormLimiter(5, keys)
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	for i := 0; i < keys; i++ {
		limiter.allow(fmt.Sprintf("fill-%d", i), now)
	}
	started := time.Now()
	for i := 0; i < keys; i++ {
		limiter.allow(fmt.Sprintf("new-%d", i), now)
	}
	// Scanning 20000 keys 20000 times takes several seconds; constant-time eviction takes milliseconds.
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("%d inserts into a full limiter took %v; eviction must not scan", keys, elapsed)
	}
	if len(limiter.index) != keys {
		t.Fatalf("limiter holds %d keys, want %d", len(limiter.index), keys)
	}
}

func TestLimiterRefusesOverTheLimitAndFreesSlotsAfterTheWindow(t *testing.T) {
	limiter := newWebFormLimiter(2, 10)
	start := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	limiter.allow("k", start)
	limiter.allow("k", start.Add(time.Second))
	if ok, wait := limiter.allow("k", start.Add(2*time.Second)); ok || wait != 58*time.Second {
		t.Fatalf("third attempt: ok=%v wait=%v, want refused with 58s to wait", ok, wait)
	}
	if ok, _ := limiter.allow("k", start.Add(61*time.Second)); !ok {
		t.Fatal("still refused after the first hit left the window")
	}
}
