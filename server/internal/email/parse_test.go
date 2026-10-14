package email_test

import (
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
`))
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
	first, err := email.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := email.Parse(raw)
	if !strings.Contains(first.Body, "Hello there") || !strings.Contains(first.Body, "Line two") || strings.Contains(first.Body, "alert") || strings.Contains(first.Body, "<") {
		t.Fatalf("html not reduced to text: %q", first.Body)
	}
	if first.MessageID == "" || first.MessageID != second.MessageID || !strings.HasSuffix(first.MessageID, "@colliedesk.invalid") {
		t.Fatalf("synthesized id not stable: %q %q", first.MessageID, second.MessageID)
	}
}

func TestAuthenticationVerdict(t *testing.T) {
	cases := map[string]string{
		"":                                    "none",
		"mx; dmarc=pass":                      "pass",
		"mx; spf=pass; dkim=pass; dmarc=fail": "fail",
		"mx; spf=fail; dkim=pass":             "pass",
		"mx; spf=softfail; dkim=fail":         "fail",
		"mx; spf=none; dkim=none":             "none",
		"mx; none":                            "none",
	}
	for header, want := range cases {
		raw := "From: a@example.com\r\nSubject: x\r\n"
		if header != "" {
			raw += "Authentication-Results: " + header + "\r\n"
		}
		parsed, err := email.Parse([]byte(raw + "\r\nbody"))
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
		"From: a@example.com\r\nAuto-Submitted: no\r\n":                                                      "",
		"From: a@example.com\r\nAuto-Submitted: auto-generated\r\n":                                          "auto_submitted",
		"From: a@example.com\r\nX-Autoreply: yes\r\n":                                                        "auto_submitted",
		"From: a@example.com\r\nPrecedence: junk\r\n":                                                        "bulk",
		"From: a@example.com\r\nList-Id: <news.example.com>\r\n":                                             "bulk",
		"From: postmaster@example.com\r\n":                                                                   "mailer_daemon",
		"From: a@example.com\r\nContent-Type: multipart/report; report-type=delivery-status; boundary=x\r\n": "mailer_daemon",
	}
	for headers, want := range cases {
		parsed, err := email.Parse([]byte(headers + "Subject: s\r\n\r\nbody"))
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
		if _, err := email.Parse([]byte(raw)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", raw)
		}
	}
}
