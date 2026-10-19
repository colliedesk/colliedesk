package mailimage_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// measure runs fn and reports how long it took and how much it allocated.
func measure(fn func()) (time.Duration, uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	started := time.Now()
	fn()
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	return elapsed, after.TotalAlloc - before.TotalAlloc
}

// One element can carry a million attributes or namespace declarations in a few megabytes. The sniff refuses
// such a document without handing it to the XML decoder, and refuses any SVG over 1 MiB before parsing.
func TestSVGSniffRefusesAttributeAndNamespaceBombsAndOversizeDocuments(t *testing.T) {
	const fiveMB = 5 << 20
	documents := map[string]string{
		"attributes":         `<svg xmlns="http://www.w3.org/2000/svg" ` + strings.Repeat(`a="1" `, fiveMB/6) + `/>`,
		"namespaces":         `<svg xmlns="http://www.w3.org/2000/svg" ` + strings.Repeat(`xmlns:a="u" `, fiveMB/12) + `/>`,
		"attributes in 1MiB": `<svg xmlns="http://www.w3.org/2000/svg" ` + strings.Repeat(`a="1" `, (1<<20)/6-10) + `/>`,
		"over 1MiB":          svgOpen + "<!--" + strings.Repeat("x", 1<<20) + "-->" + `<path d="M0 0"/></svg>`,
	}
	for name, document := range documents {
		var got string
		elapsed, allocated := measure(func() { got = mailimage.SniffImage([]byte(document)) })
		if got != "" {
			t.Errorf("%s: accepted as %q", name, got)
		}
		if elapsed > 100*time.Millisecond || allocated > 8<<20 {
			t.Errorf("%s: took %v and allocated %d MiB, want a refusal that costs almost nothing", name, elapsed, allocated>>20)
		}
	}
	few := `<svg xmlns="http://www.w3.org/2000/svg" ` + strings.Repeat(`a="1" `, 200) + `/>`
	if got := mailimage.SniffImage([]byte(few)); got != mailimage.SVGType {
		t.Errorf("200 attributes: got %q, a busy element is fine", got)
	}
	over := `<svg xmlns="http://www.w3.org/2000/svg" ` + strings.Repeat(`a="1" `, 300) + `/>`
	if got := mailimage.SniffImage([]byte(over)); got != "" {
		t.Errorf("300 attributes accepted as %q", got)
	}
}

// The same address in a message is signed once, not once per occurrence.
func TestRewriteSignsARepeatedAddressOnce(t *testing.T) {
	css := `<style>` + strings.Repeat(`.a{background:url(https://a.example/x.png)}`, 100_000) + `</style>`
	var out string
	_, allocated := measure(func() { out = signer().Rewrite(workspace, css) })
	if strings.Contains(out, "https://a.example") || len(proxiedURLs(t, out[:2000])) == 0 {
		t.Fatal("the repeated address was not rewritten")
	}
	// The output alone is about 25 MB (100000 proxy paths); signing each occurrence anew allocated 211 MiB.
	if allocated > 140<<20 {
		t.Errorf("4 MB of repeated addresses allocated %d MiB", allocated>>20)
	}
}
