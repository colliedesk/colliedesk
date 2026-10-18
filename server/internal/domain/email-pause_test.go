package domain_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func TestComplaintRateExceeded(t *testing.T) {
	cases := []struct {
		name             string
		complaints, sent int
		want             bool
	}{
		{"one complaint on a tiny base is not a pattern", 1, 2, false},
		{"two complaints on ten replies is under the minimum count", 2, 10, false},
		{"three of ten", 3, 10, true},
		{"exactly 0.3% is not over it", 3, 1000, false},
		{"just over 0.3%", 3, 999, true},
		{"many replies dilute the same complaints", 3, 5000, false},
		{"six of two thousand is exactly 0.3%", 6, 2000, false},
		{"seven of two thousand is over", 7, 2000, true},
	}
	for _, c := range cases {
		if got := domain.ComplaintRateExceeded(c.complaints, c.sent); got != c.want {
			t.Errorf("%s: ComplaintRateExceeded(%d, %d) = %v, want %v", c.name, c.complaints, c.sent, got, c.want)
		}
	}
}
