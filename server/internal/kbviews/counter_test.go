package kbviews_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/kbviews"
	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

type written struct {
	mu     sync.Mutex
	counts []kbviews.Count
	fail   error
}

func (w *written) WriteViews(_ context.Context, counts []kbviews.Count) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail != nil {
		return w.fail
	}
	w.counts = append(w.counts, counts...)
	return nil
}

func (w *written) total(article, day string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	var sum int64
	for _, c := range w.counts {
		if c.ArticleID == article && c.Day.Format(time.DateOnly) == day {
			sum += c.Views
		}
	}
	return sum
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func article(id, client string, at time.Time) pageview.Event {
	return pageview.Event{WorkspaceID: "ws", Timezone: "Asia/Ho_Chi_Minh", Kind: pageview.Article, ArticleID: id, Locale: "en",
		Client: client, UserAgent: "Mozilla/5.0 Chrome/126.0", At: at}
}

func newCounter(w kbviews.Writer, c *clock) *kbviews.Counter {
	keys := 0
	return kbviews.New(w, kbviews.Options{Now: c.Now, NewKey: func() []byte { keys++; return []byte{byte(keys)} }})
}

func TestTheSameReaderWithinHalfAnHourCountsOnce(t *testing.T) {
	// 10:00 UTC is 17:00 in Ho Chi Minh City, on 2026-11-21 in both.
	c := &clock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	w := &written{}
	counter := newCounter(w, c)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	c.now = c.now.Add(29 * time.Minute)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	counter.PageViewed(article("a1", "198.51.100.2", c.now))
	counter.PageViewed(pageview.Event{WorkspaceID: "ws", Kind: pageview.Home, Client: "198.51.100.3", At: c.now})
	if err := counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.total("a1", "2026-11-21"); got != 2 {
		t.Fatalf("views = %d, want 2 (one per reader within 30 minutes, none for the home page)", got)
	}
	c.now = c.now.Add(31 * time.Minute)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	if err := counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.total("a1", "2026-11-21"); got != 3 {
		t.Fatalf("views = %d, want 3 after the half hour passed", got)
	}
}

func TestADayChangeCountsTheSameReaderAgain(t *testing.T) {
	// 16:50 UTC on the 21st is 23:50 in Ho Chi Minh City; 17:05 UTC is 00:05 on the 22nd there.
	c := &clock{now: time.Date(2026, 11, 21, 16, 50, 0, 0, time.UTC)}
	w := &written{}
	counter := newCounter(w, c)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	c.now = time.Date(2026, 11, 21, 17, 5, 0, 0, time.UTC)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	if err := counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a, b := w.total("a1", "2026-11-21"), w.total("a1", "2026-11-22"); a != 1 || b != 1 {
		t.Fatalf("views on the 21st %d and the 22nd %d, want 1 and 1", a, b)
	}
}

func TestTheReaderKeyRotatesDaily(t *testing.T) {
	// In Honolulu both views fall on the 21st (13:50 and 14:05), so only the key rotation at UTC midnight tells them apart.
	c := &clock{now: time.Date(2026, 11, 21, 23, 50, 0, 0, time.UTC)}
	w := &written{}
	counter := newCounter(w, c)
	event := article("a1", "198.51.100.1", c.now)
	event.Timezone = "Pacific/Honolulu"
	counter.PageViewed(event)
	c.now = c.now.Add(15 * time.Minute)
	event.At = c.now
	counter.PageViewed(event)
	if err := counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.total("a1", "2026-11-21"); got != 2 {
		t.Fatalf("views = %d, want 2 after the key rotated", got)
	}
}

func TestAFailedWriteKeepsTheCounts(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	w := &written{fail: errors.New("database away")}
	counter := newCounter(w, c)
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	if err := counter.Flush(context.Background()); err == nil {
		t.Fatal("the failed write was not reported")
	}
	counter.PageViewed(article("a1", "198.51.100.2", c.now))
	w.fail = nil
	if err := counter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.total("a1", "2026-11-21"); got != 2 {
		t.Fatalf("views = %d, want 2", got)
	}
}

func TestRunFlushesOnEachTickAndOnStop(t *testing.T) {
	c := &clock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	w := &written{}
	counter := newCounter(w, c)
	ticks := make(chan time.Time)
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { counter.RunTicks(ctx, ticks); close(done) }()
	counter.PageViewed(article("a1", "198.51.100.1", c.now))
	ticks <- c.now
	ticks <- c.now // the second tick is taken only after the first flush returned
	if got := w.total("a1", "2026-11-21"); got != 1 {
		t.Fatalf("views after a tick = %d, want 1", got)
	}
	counter.PageViewed(article("a1", "198.51.100.2", c.now))
	stop()
	<-done
	if got := w.total("a1", "2026-11-21"); got != 2 {
		t.Fatalf("views after stop = %d, want 2", got)
	}
}
