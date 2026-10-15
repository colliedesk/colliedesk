package freshdesk

import (
	"fmt"
	"io"
	"sync"
	"time"
)

const defaultProgressInterval = 30 * time.Second

// progress writes one line per phase start, one line per rate-limit wait, and
// at most one counter line per interval, so an operator can tell a slow run
// from a hung one. A nil *progress is valid and writes nothing.
type progress struct {
	mu    sync.Mutex
	w     io.Writer
	every time.Duration
	start time.Time
	last  time.Time
}

func newProgress(w io.Writer, every time.Duration) *progress {
	if w == nil {
		return nil
	}
	if every <= 0 {
		every = defaultProgressInterval
	}
	now := time.Now()
	return &progress{w: w, every: every, start: now, last: now}
}

// phase announces the start of a step.
func (p *progress) phase(name string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = time.Now()
	p.line(name, p.last)
}

// tick reports done items of total (total <= 0: unknown). The last item of a
// known total is always reported; other ticks are rate limited.
func (p *progress) tick(phase string, done, total int) {
	if p == nil {
		return
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.Sub(p.last) < p.every && (total <= 0 || done < total) {
		return
	}
	p.last = now
	if total > 0 {
		p.line(fmt.Sprintf("%s %d/%d", phase, done, total), now)
		return
	}
	p.line(fmt.Sprintf("%s %d done", phase, done), now)
}

// note reports an event immediately, for example a rate-limit wait.
func (p *progress) note(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "progress: %s\n", message) //nolint:errcheck // a closed stderr must not stop an import
}

func (p *progress) line(text string, now time.Time) {
	fmt.Fprintf(p.w, "progress: %s, elapsed %s\n", text, now.Sub(p.start).Round(time.Second)) //nolint:errcheck // see note
}
