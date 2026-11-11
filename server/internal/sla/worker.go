// Package sla keeps conversations' response clocks counted (docs/user/response-targets.md).
package sla

import (
	"context"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// batch is how many stale clocks one round counts.
const batch = 500

// Worker counts the clocks the database marked stale.
type Worker struct {
	Store  *store.Store
	Logger *slog.Logger
}

// RunOnce counts the stale clocks until none is left, and returns how many it counted.
func (w Worker) RunOnce(ctx context.Context) (int, error) {
	total := 0
	for {
		counted, err := w.Store.RefreshStaleSLA(ctx, batch)
		total += counted
		if err != nil || counted < batch {
			return total, err
		}
	}
}

// Run counts the stale clocks every interval until the context ends.
func (w Worker) Run(ctx context.Context, every time.Duration) {
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			logger.Error("response clocks", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
