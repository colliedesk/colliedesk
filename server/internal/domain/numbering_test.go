package domain_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func TestANumberFormatRendersAndReadsBackItsCodes(t *testing.T) {
	for _, c := range []struct {
		format, code string
		n            int64
		year         int
	}{
		{"#{n}", "#12", 12, 2026},
		{"REF-{yyyy}-{n:04}", "REF-2026-0123", 123, 2026},
		{"REF-{yyyy}-{n:04}", "REF-2027-12345", 12345, 2027},
		{"T{n:03}", "T007", 7, 2026},
	} {
		format, err := domain.ParseNumberFormat(c.format)
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		if got := format.Render(c.n, c.year); got != c.code {
			t.Fatalf("%s renders %d as %q, want %q", c.format, c.n, got, c.code)
		}
	}
	for _, bad := range []string{"", "REF", "{n}{n}", "{yyyy}", "{n:9}", "{x}-{n}", "REF {n}\n", "{n:0}", string(make([]byte, 41))} {
		if _, err := domain.ParseNumberFormat(bad); err == nil {
			t.Fatalf("%q was taken as a format", bad)
		}
	}
}
