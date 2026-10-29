//go:build e2e

package main

import (
	"os"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// End-to-end builds can shorten a trial change (D12) so a journey sees it revert by itself without waiting
// ten minutes.
//
//	COLLIEDESK_E2E_TRIAL_SECONDS   how long a trial runs
func init() {
	if seconds, err := strconv.Atoi(os.Getenv("COLLIEDESK_E2E_TRIAL_SECONDS")); err == nil && seconds > 0 {
		httpapi.TrialDuration = time.Duration(seconds) * time.Second
	}
}
