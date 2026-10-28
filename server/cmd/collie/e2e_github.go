//go:build e2e

package main

import "os"

// End-to-end builds point the Autofix GitHub App at a local GitHub stand-in (internal/github/githubtest).
// Release builds never read this variable.
//
//	COLLIEDESK_E2E_GITHUB_URL   http://127.0.0.1:<port>
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_GITHUB_URL"); base != "" {
		githubBaseURL = base
	}
}
