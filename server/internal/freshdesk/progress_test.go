package freshdesk_test

import (
	"bytes"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunPrintsProgressLinesWithPhaseCountsAndElapsedTime(t *testing.T) {
	env := newImportEnv(t)
	var out lockedBuffer
	report, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
		o.Progress = &out
		o.ProgressInterval = time.Nanosecond
	})
	if err != nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%v err=%v", report, err)
	}
	text := out.String()
	for name, pattern := range map[string]string{
		"contacts phase":      `(?m)^progress: contacts \d+ done, elapsed \S+$`,
		"ticket id walk":      `(?m)^progress: checking ticket ids \d+/\d+, elapsed \S+$`,
		"ticket import":       `(?m)^progress: importing tickets 6/6, elapsed \S+$`,
		"every line is shape": `\A(progress: [^\n]+\n)+\z`,
	} {
		if !regexp.MustCompile(pattern).MatchString(text) {
			t.Errorf("%s: no match for %s in:\n%s", name, pattern, text)
		}
	}
}

func TestRateLimitWaitsAreCountedInReportAndAnnouncedOnProgress(t *testing.T) {
	env := newImportEnv(t)
	env.fake.throttleContacts = true
	env.fake.remainingZeroAgents = true
	var out lockedBuffer
	report, err := env.run(t, t.Context(), func(o *freshdesk.Options) { o.Progress = &out })
	if err != nil {
		t.Fatal(err)
	}
	var slept time.Duration
	for _, d := range env.sleeps {
		slept += d
	}
	limit := report.RateLimit
	if limit.Throttled != 1 || limit.Pauses < 1 {
		t.Fatalf("rate limit = %+v, want one 429 and at least one pause after remaining 0", limit)
	}
	if got := time.Duration(limit.WaitSeconds * float64(time.Second)); got != slept {
		t.Fatalf("wait = %s, want the %s the importer slept", got, slept)
	}
	if !regexp.MustCompile(`(?m)^progress: rate limit: waiting 7s \(HTTP 429\)$`).MatchString(out.String()) {
		t.Fatalf("progress does not announce the 429 wait:\n%s", out.String())
	}
	if !regexp.MustCompile(`Rate limiting: \d+ pause`).MatchString(report.Markdown()) {
		t.Fatal("markdown has no rate limiting line")
	}
}
