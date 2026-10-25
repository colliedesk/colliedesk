//go:build e2e

package main

import "os"

// End-to-end builds point Microsoft 365 connect and send at a local Microsoft stand-in
// (server/cmd/microsoft-stand-in), for both login.microsoftonline.com and graph.microsoft.com. Release
// builds never read this variable.
//
//	COLLIEDESK_E2E_MICROSOFT_URL   http://127.0.0.1:<port>
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_MICROSOFT_URL"); base != "" {
		microsoftBaseURL = base
	}
}
