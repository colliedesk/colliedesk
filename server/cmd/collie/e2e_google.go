//go:build e2e

package main

import "os"

// End-to-end builds point Gmail connect and send at a local Google stand-in (server/cmd/google-stand-in).
// Release builds never read this variable.
//
//	COLLIEDESK_E2E_GOOGLE_URL   http://127.0.0.1:<port>
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_GOOGLE_URL"); base != "" {
		googleBaseURL = base
	}
}
