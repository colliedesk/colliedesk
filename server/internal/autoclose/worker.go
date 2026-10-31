// Package autoclose closes the conversations of a chat channel that stayed resolved past the channel's
// auto_close_hours, telling their customers with the channel's closed notice.
package autoclose

import (
	"context"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Worker closes what is due, in every workspace.
type Worker struct {
	Store *store.Store
}

// batch is the most conversations one round closes.
const batch = 200

// RunOnce closes the conversations due by now and returns how many it closed.
func (w Worker) RunOnce(ctx context.Context, now time.Time) (int, error) {
	due, err := w.Store.AutoCloseDue(ctx, now, batch)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, conversation := range due {
		ok, err := w.Store.AutoClose(ctx, conversation, now)
		if err != nil {
			return closed, err
		}
		if ok {
			closed++
		}
	}
	return closed, nil
}

// Run closes what is due every interval until ctx ends.
func (w Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Warn("auto-close round failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
