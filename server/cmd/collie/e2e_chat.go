//go:build e2e

package main

import (
	"os"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

// End-to-end builds can run the loopback chat type, so a browser test meets cards of the intake queue
// made by the real ingest, journal worker and sweep. No shipped type is a chat yet; this one is the
// test stand-in of one, and only an e2e build links it.
//
//	COLLIEDESK_E2E_CHAT=1   register type "loopback": its groups (scope group) collect messages for
//	                        intake, several customers write in them, a batch closes after 2 quiet seconds
//
// It runs on a server and database of their own (web/e2e/support/chat.ts startChatServer): the web app
// knows only the shipped types, and every server on a database processes what its channels received. The
// type needs its row in channel_types (the test writes it as the database owner). A channel of it is
// created with POST /api/v1/channels {"type":"loopback","config":{"account":"<name>"}}; its public webhook
// POST /api/v1/channels/loopback/webhook takes {"account":"<name>","events":[{"id","from","name","text","group"}]}
// with no secret: the account names the channel.
func init() {
	if os.Getenv("COLLIEDESK_E2E_CHAT") == "" {
		return
	}
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Quiet, loop.Max = 2*time.Second, 20*time.Second
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {Gather: &channels.Gather{QuietSeconds: 2, MaxSeconds: 20}}}
	loop.IntakeScopes = []string{"group"}
	loop.SharedScopes = []string{"group"}
	e2eChannelTypes = []channels.Adapter{loop}
}
