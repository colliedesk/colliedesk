package httpapi

import (
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
	if got := len(limiter.hits); got > 3 {
		t.Fatalf("limiter holds %d keys, want at most 3", got)
	}
	if _, kept := limiter.hits["a"]; kept {
		t.Fatal("the oldest key was kept; it should have been evicted")
	}
	// The newest keys keep their counts.
	if _, kept := limiter.hits["c"]; !kept {
		t.Fatal("a newer key was evicted")
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
