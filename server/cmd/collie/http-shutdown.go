package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// stopServing stops the HTTP server within shutdownWithin, then writes the help center article views
// not yet written within flushWithin of their own: a shutdown that failed or used all of its time
// still writes them. The shutdown's error is returned after the write.
func stopServing(shutdown, flushViews func(context.Context) error, shutdownWithin, flushWithin time.Duration) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownWithin)
	stopErr := shutdown(stopCtx)
	cancel()
	// No page is served any more, or none will be soon: the views not yet written are written now,
	// not lost with the process.
	flushCtx, cancel := context.WithTimeout(context.Background(), flushWithin)
	defer cancel()
	if err := flushViews(flushCtx); err != nil {
		slog.Error("write help center article views at shutdown", "error", err)
	}
	if stopErr != nil {
		return fmt.Errorf("shutdown HTTP server: %w", stopErr)
	}
	return nil
}
