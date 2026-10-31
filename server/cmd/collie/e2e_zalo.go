//go:build e2e

package main

import (
	"os"

	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
)

// End-to-end builds can point the Zalo channel type at a local stand-in of Zalo's API
// (web/e2e/support/zalo.ts), so a browser test sees what a customer is told: the stand-in records every
// message sent and answers like Zalo. It is the only boundary replaced: the webhook, journal, outbox and
// the type itself are the real ones. The default addresses (openapi.zalo.me, oauth.zaloapp.com) are
// fixed in every other build.
//
//	COLLIEDESK_E2E_ZALO_URL   http://127.0.0.1:<port>: Zalo's message API and token server, both
func init() {
	if base := os.Getenv("COLLIEDESK_E2E_ZALO_URL"); base != "" {
		e2eZalo = zalo.Config{MessageBase: base, OAuthBase: base}
	}
}
