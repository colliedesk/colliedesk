package helpscout

import (
	"fmt"
	"io"
	"sync"
	"time"
)

const progressInterval = 30 * time.Second

// progress writes one line per step, one per rate-limit wait and at most one counter line every
// 30 seconds, in the same format as the Freshdesk importer. A nil *progress writes nothing.
type progress struct {
	mu          sync.Mutex
	w           io.Writer
	start, last time.Time
}

func newProgress(w io.Writer) *progress {
	if w == nil {
		return nil
	}
	now := time.Now()
	return &progress{w: w, start: now, last: now}
}

func (p *progress) phase(name string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = time.Now()
	p.line(name)
}

// tick reports done items of total; the last item is always reported.
func (p *progress) tick(phase string, done, total int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.last) < progressInterval && (total <= 0 || done < total) {
		return
	}
	p.last = time.Now()
	if total <= 0 {
		p.line(fmt.Sprintf("%s %d done", phase, done))
		return
	}
	p.line(fmt.Sprintf("%s %d/%d", phase, done, total))
}

func (p *progress) note(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "progress: %s\n", message) //nolint:errcheck // a closed stderr must not stop an import
}

func (p *progress) line(text string) {
	fmt.Fprintf(p.w, "progress: %s, elapsed %s\n", text, time.Since(p.start).Round(time.Second)) //nolint:errcheck // see note
}
