//go:build e2e

package main

import (
	"os"
	"strconv"
	"time"
)

// End-to-end builds can write help center article views sooner, so a journey sees a view in Results
// without waiting 30 seconds.
//
//	COLLIEDESK_E2E_VIEW_FLUSH_MS   how often views are written
func init() {
	if ms, err := strconv.Atoi(os.Getenv("COLLIEDESK_E2E_VIEW_FLUSH_MS")); err == nil && ms > 0 {
		viewFlushEvery = time.Duration(ms) * time.Millisecond
	}
}
