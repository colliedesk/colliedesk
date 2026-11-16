package httpapi_test

import (
	"testing"
	"time"
)

// bestOf runs measure up to n times and returns the fastest result, stopping at the first one within
// budget. Noise on a shared CI runner (other processes, PostgreSQL on the same machine, a garbage
// collection) only ever adds time, so the fastest of a few runs is the best estimate of what the code
// costs: code that is really too slow stays over budget on every run, and the gate still fails. It can
// hide a cost that comes only now and then (lock contention, a cold cache on the first call); a gate
// that must catch that measures it on its own instead.
func bestOf(n int, budget time.Duration, measure func() time.Duration) time.Duration {
	fastest := measure()
	for run := 1; run < n && fastest > budget; run++ {
		fastest = min(fastest, measure())
	}
	return fastest
}

func TestBestOfReturnsTheFastestOfUpToNMeasurements(t *testing.T) {
	const budget = 200 * time.Millisecond
	for _, c := range []struct {
		name     string
		measured []time.Duration
		want     time.Duration
		calls    int
	}{
		{"a slow run then one within budget stops there", []time.Duration{500 * time.Millisecond, 120 * time.Millisecond, 90 * time.Millisecond}, 120 * time.Millisecond, 2},
		{"a first run within budget measures once", []time.Duration{80 * time.Millisecond, 10 * time.Millisecond}, 80 * time.Millisecond, 1},
		{"over budget every time keeps the fastest of n", []time.Duration{900 * time.Millisecond, 700 * time.Millisecond, 800 * time.Millisecond, 50 * time.Millisecond}, 700 * time.Millisecond, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			got := bestOf(3, budget, func() time.Duration {
				calls++
				return c.measured[calls-1]
			})
			if got != c.want || calls != c.calls {
				t.Fatalf("bestOf = %s after %d measurements, want %s after %d", got, calls, c.want, c.calls)
			}
		})
	}
}
