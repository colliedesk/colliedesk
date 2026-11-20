package channelemail_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	mailpkg "github.com/colliedesk/colliedesk/server/internal/email"
)

// Mail read from a Gmail inbox carries Gmail's verdict, and keeps it when a held message is parsed again
// from the journal; the server's own trusted authserv-id is another one.
func TestMailReadFromGmailKeepsGmailsVerdictAlsoWhenParsedAgain(t *testing.T) {
	raw := []byte("Authentication-Results: mx.google.com; spf=pass smtp.mailfrom=customer.example\r\nFrom: olive@customer.example\r\n" +
		"To: support@acme.example\r\nSubject: Hi\r\nMessage-ID: <gmail-read@customer.example>\r\n\r\nHello.\r\n")
	adapter := channelemail.New(channelemail.Config{TrustedAuthServID: "mx.colliedesk.example"})
	parsed, err := mailpkg.Parse(raw, mailpkg.GmailAuthServID)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Gmail, parsed.Direct = true, true
	first, err := adapter.Inbound().Parse(context.Background(), channels.Channel{}, nil, channels.Raw{Parsed: parsed, Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	if meta := first[0].EmailMeta; meta.Via != domain.ViaGmail || meta.Authentication != mailpkg.AuthPass {
		t.Fatalf("read from Gmail: %+v", meta)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(raw)
	_ = writer.Close()
	again, err := adapter.Inbound().Parse(context.Background(), channels.Channel{}, nil, channels.Raw{JSON: json.RawMessage(first[0].Summary), Body: compressed.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	if meta := again[0].EmailMeta; meta.Via != domain.ViaGmail || meta.Authentication != mailpkg.AuthPass {
		t.Fatalf("parsed again from the journal: %+v", meta)
	}
	// The same bytes forwarded to the server are judged by its own authserv-id: Gmail's header is the sender's.
	forwarded, _ := mailpkg.Parse(raw, "mx.colliedesk.example")
	if forwarded.Authentication != mailpkg.AuthNone {
		t.Fatalf("a forwarded copy believed mx.google.com: %s", forwarded.Authentication)
	}
}
