package kbformat_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/kbformat"
)

// The web editor reads the same cases (web/src/features/kb/editor), so it never writes a body the
// server would rewrite.
func TestNormalizeWritesEachFormattingTagOneWay(t *testing.T) {
	raw, err := os.ReadFile("testdata/normalize.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, In, Out string }
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := kbformat.Normalize(c.In); got != c.Out {
				t.Errorf("Normalize(%q)\n got %q\nwant %q", c.In, got, c.Out)
			}
			if again := kbformat.Normalize(c.Out); again != c.Out {
				t.Errorf("Normalize is not stable on %q: %q", c.Out, again)
			}
		})
	}
}
