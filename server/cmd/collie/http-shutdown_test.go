package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/kbviews"
	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// liveWriter keeps the counts it is given, and refuses them on a context that has ended, as the
// database does.
type liveWriter struct {
	mu    sync.Mutex
	views int64
}

func (w *liveWriter) WriteKBArticleViews(ctx context.Context, counts []kbviews.Count) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range counts {
		w.views += c.Views
	}
	return nil
}

func TestAShutdownThatFailedStillWritesThePendingViews(t *testing.T) {
	writer := &liveWriter{}
	views := kbviews.New(writer, kbviews.Options{})
	views.PageViewed(pageview.Event{WorkspaceID: "ws", Kind: pageview.Article, ArticleID: "a1", Locale: "en",
		Client: "198.51.100.7", UserAgent: "Mozilla/5.0 Chrome/126.0", At: time.Now()})
	// A connection that never goes idle: Shutdown uses all of its time and fails.
	stuck := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	err := stopServing(stuck, views.Flush, 20*time.Millisecond, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop = %v, want the shutdown's deadline error", err)
	}
	if writer.views != 1 {
		t.Fatalf("views written = %d, want 1", writer.views)
	}
}
