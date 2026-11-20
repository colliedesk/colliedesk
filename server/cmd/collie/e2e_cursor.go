//go:build e2e

package main

import (
	"os"
	"strconv"
	"time"
)

// End-to-end builds point Autofix's Cursor calls at a local Cursor stand-in (cursortest), and can read a
// sent run sooner than every 15 seconds. Release builds never read these variables.
//
//	COLLIEDESK_E2E_CURSOR_URL            http://127.0.0.1:<port>
//	COLLIEDESK_E2E_CURSOR_READ_SECONDS   how often the worker looks for due fixes and rereads a run
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_CURSOR_URL"); base != "" {
		cursorBaseURL = base
	}
	if seconds, err := strconv.Atoi(os.Getenv("COLLIEDESK_E2E_CURSOR_READ_SECONDS")); err == nil && seconds > 0 {
		cursorReadEvery = time.Duration(seconds) * time.Second
	}
}
