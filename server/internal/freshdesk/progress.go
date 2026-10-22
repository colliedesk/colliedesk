package freshdesk

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const defaultProgressInterval = 30 * time.Second

// ProgressEvent is one structured progress report of a run (Options.OnProgress).
type ProgressEvent struct {
	// Phase is the step running, as the progress lines name it ("importing tickets").
	Phase string
	// Done and Total count the items of the phase; Total is 0 when unknown.
	Done, Total int
	// RateLimitUntil is set when the run starts waiting for the Freshdesk rate limit.
	RateLimitUntil *time.Time
	// APICalls counts the Freshdesk requests of the run so far, retries and file downloads included.
	APICalls int
}

// progress writes one line per phase start, one line per rate-limit wait, and
// at most one counter line per interval, so an operator can tell a slow run
// from a hung one, and passes every event to the OnProgress callback.
// A nil *progress is valid and reports nothing.
type progress struct {
	mu    sync.Mutex
	w     io.Writer
	every time.Duration
	start time.Time
	last  time.Time
	on    func(ProgressEvent)
	calls *atomic.Int64
	// current is the phase of the last phase or tick, for rate-limit events.
	current string
}

func newProgress(w io.Writer, every time.Duration, on func(ProgressEvent)) *progress {
	if w == nil && on == nil {
		return nil
	}
	if every <= 0 {
		every = defaultProgressInterval
	}
	now := time.Now()
	return &progress{w: w, every: every, start: now, last: now, on: on, calls: &atomic.Int64{}}
}

// silent returns a progress that reports the same events and API calls to the callback but writes
// no lines: the sizing pass's own steps are not the operator's.
func (p *progress) silent() *progress {
	if p == nil || p.on == nil {
		return nil
	}
	return &progress{every: p.every, start: p.start, last: p.last, on: p.on, calls: p.calls}
}

// call counts one Freshdesk request.
func (p *progress) call() {
	if p != nil {
		p.calls.Add(1)
	}
}

func (p *progress) emit(event ProgressEvent) {
	if p == nil || p.on == nil {
		return
	}
	event.APICalls = int(p.calls.Load())
	p.on(event)
}

// phase announces the start of a step.
func (p *progress) phase(name string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.last, p.current = time.Now(), name
	p.line(name, p.last)
	p.mu.Unlock()
	p.emit(ProgressEvent{Phase: name})
}

// rateLimitWait announces a wait for the Freshdesk rate limit.
func (p *progress) rateLimitWait(d time.Duration, cause string) {
	if p == nil {
		return
	}
	p.note(fmt.Sprintf("rate limit: waiting %s (%s)", d, cause))
	until := time.Now().Add(d).UTC()
	p.mu.Lock()
	current := p.current
	p.mu.Unlock()
	p.emit(ProgressEvent{Phase: current, RateLimitUntil: &until})
}

// tick reports done items of total (total <= 0: unknown). The last item of a
// known total is always written; other lines are rate limited. The callback gets every tick.
func (p *progress) tick(phase string, done, total int) {
	if p == nil {
		return
	}
	p.emit(ProgressEvent{Phase: phase, Done: done, Total: total})
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = phase
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
	if p.w == nil {
		return
	}
	fmt.Fprintf(p.w, "progress: %s\n", message) //nolint:errcheck // a closed stderr must not stop an import
}

func (p *progress) line(text string, now time.Time) {
	if p.w == nil {
		return
	}
	fmt.Fprintf(p.w, "progress: %s, elapsed %s\n", text, now.Sub(p.start).Round(time.Second)) //nolint:errcheck // see note
}
