// Package kbviews counts help center article views in memory and writes the counts to
// kb_article_views on a timer, so a page never waits on the database for its view
// (docs/internals/knowledge-base.md › Views and uses).
package kbviews

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"log/slog"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
	"github.com/colliedesk/colliedesk/server/internal/results"
)

// Window is how long repeat views of one article by one reader count once.
const Window = 30 * time.Minute

// FlushEvery is how often the counts are written; a crash loses at most this much.
const FlushEvery = 30 * time.Second

// maxReaders bounds the readers remembered for Window, so rotating user agents cannot grow the memory.
// A view by a reader that cannot be remembered is not counted, so it cannot count once per page.
const maxReaders = 200_000

// pruneFullEvery is how often a full memory looks for expired readers, so a flood of new readers
// does not scan it on every page.
const pruneFullEvery = time.Minute

// Count is the views of one article in one locale on one day of the workspace's timezone.
type Count struct {
	WorkspaceID, ArticleID, Locale string
	// Day is the calendar day in the workspace timezone, at 00:00 UTC.
	Day   time.Time
	Views int64
}

// Writer adds counts to the ones stored.
type Writer interface {
	WriteKBArticleViews(ctx context.Context, counts []Count) error
}

// Options are the counter's clock and key source; zero values use the real ones.
type Options struct {
	Now func() time.Time
	// NewKey returns the key the reader hashes of one UTC day are made with; it is kept in memory only.
	NewKey func() []byte
	// MaxReaders bounds the readers remembered at once; 0 is maxReaders.
	MaxReaders int
}

type reader struct {
	article string
	hash    [sha256.Size]byte
}

// Counter is the article view counter; it is a pageview.Consumer.
type Counter struct {
	writer Writer
	now    func() time.Time
	newKey func() []byte
	limit  int

	mu      sync.Mutex
	keyDay  string
	key     []byte
	seen    map[reader]time.Time
	pending map[Count]int64
	// prunedAt is when the readers were last pruned; dropped counts the views not counted since the
	// last flush because the memory was full.
	prunedAt time.Time
	dropped  int64
	// flushing serializes Flush, so a timer flush and the last one never write the same counts twice.
	flushing sync.Mutex
}

// New returns a counter writing to writer.
func New(writer Writer, options Options) *Counter {
	c := &Counter{writer: writer, now: options.Now, newKey: options.NewKey, limit: options.MaxReaders, seen: map[reader]time.Time{}, pending: map[Count]int64{}}
	if c.now == nil {
		c.now = func() time.Time { return time.Now().UTC() }
	}
	if c.newKey == nil {
		c.newKey = func() []byte { return []byte(rand.Text()) }
	}
	if c.limit <= 0 {
		c.limit = maxReaders
	}
	return c
}

// PageViewed counts an article event once per reader within Window. It only touches memory.
func (c *Counter) PageViewed(event pageview.Event) {
	if event.Kind != pageview.Article || event.ArticleID == "" || event.WorkspaceID == "" {
		return
	}
	now := c.now()
	day := localDay(event.At, event.Timezone)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rotate(now)
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(event.Client + "\n" + event.UserAgent + "\n" + day.Format(time.DateOnly)))
	who := reader{article: event.ArticleID}
	copy(who.hash[:], mac.Sum(nil))
	last, known := c.seen[who]
	if known && now.Sub(last) < Window {
		return
	}
	if !known && len(c.seen) >= c.limit && now.Sub(c.prunedAt) >= pruneFullEvery {
		c.pruneLocked(now)
	}
	if !known && len(c.seen) >= c.limit {
		c.dropped++
		return
	}
	c.seen[who] = now
	c.pending[Count{WorkspaceID: event.WorkspaceID, ArticleID: event.ArticleID, Locale: event.Locale, Day: day}]++
}

// rotate makes a new key at the first view of each UTC day and forgets the readers of the old one,
// whose hashes nothing can match any more. Readers older than Window are forgotten too.
func (c *Counter) rotate(now time.Time) {
	if day := now.UTC().Format(time.DateOnly); day != c.keyDay {
		c.keyDay, c.key, c.seen = day, c.newKey(), map[reader]time.Time{}
	}
}

// pruneLocked forgets the readers older than Window; c.mu is held.
func (c *Counter) pruneLocked(now time.Time) {
	c.prunedAt = now
	for who, last := range c.seen {
		if now.Sub(last) >= Window {
			delete(c.seen, who)
		}
	}
}

// Flush writes the counts not yet written in one call to the writer. It holds no lock a page needs
// while it writes: the counts are taken first, and put back when the write fails.
func (c *Counter) Flush(ctx context.Context) error {
	c.flushing.Lock()
	defer c.flushing.Unlock()
	c.mu.Lock()
	c.pruneLocked(c.now())
	taken, dropped := c.pending, c.dropped
	c.pending, c.dropped = map[Count]int64{}, 0
	c.mu.Unlock()
	if dropped > 0 {
		slog.Warn("help center article views not counted: too many readers to remember", "views", dropped, "max_readers", c.limit)
	}
	if len(taken) == 0 {
		return nil
	}
	counts := make([]Count, 0, len(taken))
	for key, views := range taken {
		key.Views = views
		counts = append(counts, key)
	}
	err := c.writer.WriteKBArticleViews(ctx, counts)
	if err != nil {
		c.mu.Lock()
		for key, views := range taken {
			c.pending[key] += views
		}
		c.mu.Unlock()
	}
	return err
}

// Run flushes every interval (FlushEvery when 0) until ctx ends, then once more.
func (c *Counter) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		interval = FlushEvery
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.runTicks(ctx, ticker.C, onError)
}

func (c *Counter) runTicks(ctx context.Context, ticks <-chan time.Time, onError func(error)) {
	report := func(err error) {
		if err != nil && onError != nil {
			onError(err)
		}
	}
	for {
		select {
		case <-ticks:
			report(c.Flush(ctx))
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			report(c.Flush(last))
			cancel()
			return
		}
	}
}

// localDay is the calendar day of at in the timezone, as 00:00 UTC of that date.
func localDay(at time.Time, timezone string) time.Time {
	local := at.In(results.Location(timezone))
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
