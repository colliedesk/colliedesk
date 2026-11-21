package kbviews

import (
	"context"
	"time"
)

// RunTicks is Run on a tick channel the test drives.
func (c *Counter) RunTicks(ctx context.Context, ticks <-chan time.Time) { c.runTicks(ctx, ticks, nil) }
