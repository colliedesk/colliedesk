package mailimage_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg">`

// An outside sender chooses the bytes behind every image address. A 5 MB document of nested elements must be
// refused at the first violation, not parsed to the end: the parse of 1.7 million levels cost about 170 MB.
func TestSVGSniffRefusesDeepNestingQuicklyAndWithBoundedMemory(t *testing.T) {
	const fiveMB = 5 << 20
	documents := map[string]string{
		"unclosed":      svgOpen + strings.Repeat("<g>", fiveMB/3),
		"closed":        svgOpen + strings.Repeat("<g>", fiveMB/7) + strings.Repeat("</g>", fiveMB/7) + "</svg>",
		"just too deep": svgOpen + strings.Repeat("<g>", 70) + strings.Repeat("</g>", 70) + "</svg>",
	}
	for name, document := range documents {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		started := time.Now()
		got := mailimage.SniffImage([]byte(document))
		elapsed := time.Since(started)
		runtime.ReadMemStats(&after)
		if got != "" {
			t.Errorf("%s: accepted as %q", name, got)
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("%s: took %v, the walk must stop at the first violation", name, elapsed)
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
			t.Errorf("%s: allocated %d MiB", name, allocated>>20)
		}
	}
}

func TestSVGSniffRefusesTooManyElementsAndKeepsRealisticDocuments(t *testing.T) {
	flat := func(n int) string { return svgOpen + strings.Repeat("<path d=\"M0 0\"/>", n) + "</svg>" }
	if got := mailimage.SniffImage([]byte(flat(60_000))); got != "" {
		t.Errorf("60000 elements accepted as %q", got)
	}
	if got := mailimage.SniffImage([]byte(flat(5_000))); got != mailimage.SVGType {
		t.Errorf("5000 elements: got %q, a busy icon sheet is fine", got)
	}
	nested := svgOpen + strings.Repeat("<g>", 40) + `<path d="M0 0"/>` + strings.Repeat("</g>", 40) + "</svg>"
	if got := mailimage.SniffImage([]byte(nested)); got != mailimage.SVGType {
		t.Errorf("40 levels: got %q", got)
	}
}
