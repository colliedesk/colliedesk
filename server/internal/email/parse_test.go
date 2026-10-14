package email_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func crlf(value string) []byte { return []byte(strings.ReplaceAll(value, "\n", "\r\n")) }

func TestParseDecodesHeadersCharsetsAndThreadIDs(t *testing.T) {
	parsed, err := email.Parse(crlf(`From: =?UTF-8?Q?Jos=C3=A9_Ram=C3=ADrez?= <Jose@Example.COM>
To: Support <support+r-0123456789ABCDEF0123456789abcdef@desk.example>, other@example.org
Cc: copy@example.org
Subject: =?ISO-8859-1?Q?R=E9sum=E9?=
Message-ID:   <abc@mail.example>
In-Reply-To: <parent@desk.example> (comment)
References: <root@desk.example>
	<parent@desk.example>
Content-Type: text/plain; charset=ISO-8859-1
Content-Transfer-Encoding: quoted-printable

Ca va tr=E8s bien.
`), "")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FromAddress != "jose@example.com" || parsed.FromName != "José Ramírez" {
		t.Fatalf("from=%q name=%q", parsed.FromAddress, parsed.FromName)
	}
	if parsed.Subject != "Résumé" || strings.TrimSpace(parsed.Body) != "Ca va très bien." {
		t.Fatalf("subject=%q body=%q", parsed.Subject, parsed.Body)
	}
	if parsed.MessageID != "abc@mail.example" || len(parsed.InReplyTo) != 1 || parsed.InReplyTo[0] != "parent@desk.example" {
		t.Fatalf("ids: %q %q", parsed.MessageID, parsed.InReplyTo)
	}
	if strings.Join(parsed.References, ",") != "root@desk.example,parent@desk.example" {
		t.Fatalf("references=%q", parsed.References)
	}
	if token := email.ReplyToken(parsed.Recipients); token != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("reply token=%q", token)
	}
}

func TestParseFallsBackToHTMLAndSynthesizesMessageID(t *testing.T) {
	raw := crlf(`From: a@example.com
Subject: html only
MIME-Version: 1.0
Content-Type: text/html; charset=utf-8

<html><body><p>Hello&nbsp;<b>there</b></p><script>alert(1)</script><p>Line two</p></body></html>
`)
	first, err := email.Parse(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := email.Parse(raw, "")
	if !strings.Contains(first.Body, "Hello there") || !strings.Contains(first.Body, "Line two") || strings.Contains(first.Body, "alert") || strings.Contains(first.Body, "<") {
		t.Fatalf("html not reduced to text: %q", first.Body)
	}
	if first.MessageID == "" || first.MessageID != second.MessageID || !strings.HasSuffix(first.MessageID, "@colliedesk.invalid") {
		t.Fatalf("synthesized id not stable: %q %q", first.MessageID, second.MessageID)
	}
}

func TestAuthenticationVerdict(t *testing.T) {
	cases := map[string]string{
		"":                           "none",
		"mx.example.net; dmarc=pass": "pass",
		"mx.example.net; spf=pass; dkim=pass; dmarc=fail": "fail",
		"mx.example.net; spf=fail; dkim=pass":             "pass",
		"mx.example.net; spf=softfail; dkim=fail":         "fail",
		"mx.example.net; spf=none; dkim=none":             "none",
		"mx.example.net; none":                            "none",
		"MX.Example.NET 1; dmarc=pass":                    "pass",
		"attacker.example; dmarc=pass":                    "none",
	}
	for header, want := range cases {
		raw := "From: a@example.com\r\nSubject: x\r\n"
		if header != "" {
			raw += "Authentication-Results: " + header + "\r\n"
		}
		parsed, err := email.Parse([]byte(raw+"\r\nbody"), "mx.example.net")
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Authentication != want {
			t.Errorf("%q => %q, want %q", header, parsed.Authentication, want)
		}
	}
}

func TestDropReason(t *testing.T) {
	cases := map[string]string{
		"From: a@example.com\r\nAuto-Submitted: no\r\n":             "",
		"From: a@example.com\r\nAuto-Submitted: auto-generated\r\n": "auto_submitted",
		"From: a@example.com\r\nX-Autoreply: yes\r\n":               "auto_submitted",
		"From: a@example.com\r\nPrecedence: junk\r\n":               "bulk",
		"From: a@example.com\r\nList-Id: <news.example.com>\r\n":    "",
		// Google Groups stamps every message to support@ with list headers; it must be ingested.
		"From: a@example.com\r\nList-Id: <support.example.com>\r\nPrecedence: list\r\nMailing-list: list support@example.com\r\nList-Unsubscribe: <mailto:googlegroups-manage+1+unsubscribe@googlegroups.com>\r\n": "",
		"From: a@example.com\r\nList-Id: <news.example.com>\r\nPrecedence: bulk\r\n":                                                                                    "bulk",
		"From: a@example.com\r\nList-Id: <news.example.com>\r\nList-Unsubscribe: <https://news.example.com/u>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n": "bulk",
		"From: a@example.com\r\nList-Id: <news.example.com>\r\nFeedback-ID: 1:campaign:esp\r\n":                                                                         "bulk",
		"From: postmaster@example.com\r\n": "mailer_daemon",
		"From: a@example.com\r\nContent-Type: multipart/report; report-type=delivery-status; boundary=x\r\n": "mailer_daemon",
	}
	for headers, want := range cases {
		parsed, err := email.Parse([]byte(headers+"Subject: s\r\n\r\nbody"), "")
		if err != nil {
			t.Fatal(err)
		}
		if parsed.DropReason != want {
			t.Errorf("%q => %q, want %q", headers, parsed.DropReason, want)
		}
	}
}

func TestParseRejectsMessagesWithoutSender(t *testing.T) {
	for _, raw := range []string{"garbage", "Subject: x\r\n\r\nbody", "From: not an address\r\n\r\nbody"} {
		if _, err := email.Parse([]byte(raw), ""); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", raw)
		}
	}
}

func TestForgedAuthenticationResultsAreNotTrusted(t *testing.T) {
	raw := []byte("From: ceo@customer.example\r\nAuthentication-Results: mx.example.net; dmarc=pass\r\nSubject: x\r\n\r\nbody")
	for _, trusted := range []string{"", "mx.cloudflare.net"} {
		parsed, err := email.Parse(raw, trusted)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Authentication != "none" {
			t.Errorf("trusted=%q: sender-supplied dmarc=pass => %q, want none", trusted, parsed.Authentication)
		}
	}
	stamped := []byte("From: ceo@customer.example\r\nAuthentication-Results: mx.cloudflare.net; dmarc=fail\r\n" +
		"Authentication-Results: mx.cloudflare.net; dmarc=pass\r\nSubject: x\r\n\r\nbody")
	if parsed, _ := email.Parse(stamped, "mx.cloudflare.net"); parsed.Authentication != "fail" {
		t.Errorf("topmost trusted stamp must win, got %q", parsed.Authentication)
	}
}

func TestParseBoundsHostileThreadHeaders(t *testing.T) {
	long := strings.Repeat("a", 3000) + "@example.com"
	var many strings.Builder
	for index := range 5000 {
		fmt.Fprintf(&many, "<id-%d@example.com> ", index)
	}
	raw := []byte("From: a@example.com\r\nMessage-ID: <" + long + ">\r\nIn-Reply-To: " + many.String() +
		"\r\nReferences: " + many.String() + "<" + long + ">\r\nSubject: x\r\n\r\nbody")
	parsed, err := email.Parse(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.MessageID) > 255 || !strings.HasSuffix(parsed.MessageID, "@colliedesk.invalid") {
		t.Fatalf("long Message-ID not bounded: %d bytes", len(parsed.MessageID))
	}
	again, _ := email.Parse(raw, "")
	if again.MessageID != parsed.MessageID {
		t.Fatal("bounded Message-ID must be deterministic")
	}
	if len(parsed.InReplyTo) > 10 || len(parsed.References) > 50 {
		t.Fatalf("thread ids not capped: in-reply-to=%d references=%d", len(parsed.InReplyTo), len(parsed.References))
	}
	if last := parsed.References[len(parsed.References)-1]; last != parsed.MessageID {
		t.Fatalf("long reference must map to the same bounded id: %q", last)
	}
}

func TestParseMarksTruncatedText(t *testing.T) {
	body := strings.Repeat("x", 6<<20)
	parsed, err := email.Parse([]byte("From: a@example.com\r\nSubject: big\r\n\r\n"+body), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(parsed.Body, "[Message text truncated at 5 MiB]") {
		t.Fatalf("truncation is silent: ends with %q", parsed.Body[len(parsed.Body)-40:])
	}
}

func TestSynthesizedMessageIDIgnoresTraceHeaders(t *testing.T) {
	message := "From: a@example.com\r\nSubject: no id\r\nDate: Wed, 14 Oct 2026 08:00:00 +0000\r\n\r\nSame body."
	first, _ := email.Parse([]byte("Received: from relay-1\r\n"+message), "")
	second, _ := email.Parse([]byte("Received: from relay-2\r\nReceived: from relay-3\r\n"+message), "")
	if first.MessageID != second.MessageID {
		t.Fatalf("redelivery through another hop changed the id: %q vs %q", first.MessageID, second.MessageID)
	}
}

func TestComposeFoldsLongSubjectsAndRejectsBadRecipients(t *testing.T) {
	message, err := email.Compose(email.Outgoing{FromAddress: "support@example.com", ToAddress: "a@example.com", MessageID: "m@example.com",
		Subject: strings.Repeat("Ünïcödé subject ", 300), Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(message), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("header line of %d bytes exceeds RFC 5322 limit", len(line))
		}
	}
	if _, err := email.Compose(email.Outgoing{FromAddress: "support@example.com", ToAddress: "not an address", MessageID: "m@example.com"}); err == nil {
		t.Fatal("an invalid recipient must fail composition")
	}
}

func TestSMTPConfigRejectsImplicitTLSPort(t *testing.T) {
	env := map[string]string{"SMTP_HOST": "smtp.example.net", "SMTP_PORT": "465"}
	if _, err := email.SMTPConfigFromEnv(func(key string) string { return env[key] }); err == nil || !strings.Contains(err.Error(), "587") {
		t.Fatalf("port 465 must be rejected with guidance, got %v", err)
	}
}
