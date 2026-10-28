//go:build e2e

package main

import "os"

// End-to-end builds point Autofix's Cursor calls at a local Cursor stand-in (cursortest). Release builds
// never read this variable.
//
//	COLLIEDESK_E2E_CURSOR_URL   http://127.0.0.1:<port>
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_CURSOR_URL"); base != "" {
		cursorBaseURL = base
	}
}
