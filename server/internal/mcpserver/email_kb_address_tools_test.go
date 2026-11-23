package mcpserver_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// An AI agent reads the workspace's knowledge base address in the same call that names the hosted
// forwarding address, so it can tell a person where to forward a mail that should become an article.
// A server that does not offer the address answers null.
func TestTheEmailChannelToolNamesTheKnowledgeBaseAddress(t *testing.T) {
	hosted := newEnvWith(t, func(config *httpapi.Config) {
		config.Email.HostedMailDomain = "desk.colliedesk.test"
		config.Email.PlatformInboundSecret = "platform-inbound-secret-0123456789abcdef"
		config.Email.TrustedAuthServID = "mx.example.net"
	})
	w := hosted.newWorkspace("KB Address Tools", "admin@kb-address-tools.example.com")
	session := hosted.connect(w.adminTier, "")
	got := hosted.ok(session, "colliedesk_email_channel_get", nil)
	if address := at(t, got, "email_channel", "kb_address"); address != "kb@kb-address-tools.desk.colliedesk.test" {
		t.Fatalf("kb_address = %v, want kb@kb-address-tools.desk.colliedesk.test", address)
	}

	plain := newEnv(t)
	own := plain.newWorkspace("KB Address Plain", "admin@kb-address-plain.example.com")
	got = plain.ok(plain.connect(own.adminTier, ""), "colliedesk_email_channel_get", nil)
	if channel, _ := got["email_channel"].(map[string]any); channel == nil {
		t.Fatalf("no email_channel in %v", got)
	} else if address, present := channel["kb_address"]; !present || address != nil {
		t.Fatalf("kb_address on a server without hosted mail = %v (present %v), want null", address, present)
	}
}
