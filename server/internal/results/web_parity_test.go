package results

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The results page tells a person how many conversations a number still needs. That figure lives in the web app
// (the API carries counts, not the rule), so it must stay equal to MinSample.
func TestWebMinSampleMatchesMinSample(t *testing.T) {
	const web = "web/src/features/results/results-format.ts"
	source, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(web)))
	if err != nil {
		t.Fatalf("read %s: %v", web, err)
	}
	found := regexp.MustCompile(`(?m)^export const minSample = (\d+);`).FindSubmatch(source)
	if found == nil {
		t.Fatalf("%s has no `export const minSample = <number>;` line: the page needs it to say what a number still needs", web)
	}
	got, err := strconv.Atoi(string(found[1]))
	if err != nil {
		t.Fatalf("%s: minSample %q is not a number: %v", web, found[1], err)
	}
	if got != MinSample {
		t.Fatalf("minSample is %d in %s but MinSample is %d in server/internal/results/metrics.go: the page would tell people to wait for the wrong number of conversations; change them together", got, web, MinSample)
	}
}
