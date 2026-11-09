package httpapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailboxquiet"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A support address that got mail every day and none for 25 hours needs attention in the channel API,
// which the MCP tools colliedesk_channels_list and _get answer with; the next email through the inbound
// route clears it. Another workspace's channel is untouched.
func TestAQuietMailboxNeedsAttentionInTheChannelAPIUntilTheNextEmail(t *testing.T) {
	var config httpapi.Config
	api, service, pool, _ := testAPIWithConfig(t, func(c *httpapi.Config) { config = *c })
	admin := createSession(t, api, service, "Quiet Desk", "admin@quiet-desk.example.com")
	secret := connectEmail(t, api, admin, "support@quiet-desk.example.com")
	inboundOK(t, api, secret, mail([]string{"From: first@customer.example", "To: support@quiet-desk.example.com",
		"Subject: First", "Message-ID: <first@customer.example>"}, "Hello."))
	other := createSession(t, api, service, "Busy Desk", "admin@busy-desk.example.com")
	otherSecret := connectEmail(t, api, other, "support@busy-desk.example.com")
	inboundOK(t, api, otherSecret, mail([]string{"From: first@customer.example", "To: support@busy-desk.example.com",
		"Subject: First", "Message-ID: <busy-first@customer.example>"}, "Hello."))

	// The email that arrived is the last of seven days of mail, 25 hours ago.
	ctx := context.Background()
	items, _ := listChannels(t, api, admin)
	channelID := channelOf(t, items, "email").ID
	if _, err := pool.Exec(ctx, `UPDATE channel_inbound_events SET received_at=now()-interval '25 hours' WHERE channel_id=$1`, channelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_inbound_events(workspace_id,channel_id,state,raw,received_at)
		SELECT workspace_id,id,'processed','{}',now()-interval '25 hours'-make_interval(days => d) FROM channels, generate_series(1,6) d WHERE id=$1`, channelID); err != nil {
		t.Fatal(err)
	}
	worker := mailboxquiet.Worker{Store: store.New(pool).WithChannels(httpapi.Channels(config, httpapi.Outbound(config), helpchat.Config{}))}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	items, _ = listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	if email.Status.State != "needs_attention" || email.Status.ReasonCode == nil || *email.Status.ReasonCode != "no_recent_mail" || email.Status.Message == nil {
		t.Fatalf("after 25 hours without mail: %+v, want needs_attention / no_recent_mail with a message", email.Status)
	}
	// This server has no hosted forwarding address: the message says to check the forwarding to Colliedesk.
	if message := *email.Status.Message; !strings.HasPrefix(message, "No email has arrived at support@quiet-desk.example.com since ") ||
		!strings.HasSuffix(message, ". Check that its mail is still forwarded to Colliedesk. This clears by itself when the next email arrives.") {
		t.Fatalf("message = %q", message)
	}
	detail := request(t, api, "GET", "/api/v1/channels/"+channelID, nil, admin, nil)
	if !strings.Contains(detail.Body.String(), `"reason_code":"no_recent_mail"`) {
		t.Fatalf("GET the channel: %s", detail.Body.String())
	}
	otherItems, _ := listChannels(t, api, other)
	if got := channelOf(t, otherItems, "email"); got.Status.State != "working" {
		t.Fatalf("another workspace's channel: %+v, want working", got.Status)
	}
	if denied := request(t, api, "GET", "/api/v1/channels/"+channelID, nil, other, nil); denied.Code != 404 {
		t.Fatalf("another workspace reads the quiet channel: %d", denied.Code)
	}

	inboundOK(t, api, secret, mail([]string{"From: second@customer.example", "To: support@quiet-desk.example.com",
		"Subject: Back", "Message-ID: <second@customer.example>"}, "Hello again."))
	items, _ = listChannels(t, api, admin)
	if got := channelOf(t, items, "email"); got.Status.State != "working" || got.Status.ReasonCode != nil {
		t.Fatalf("after the next email: %+v, want working", got.Status)
	}
}
