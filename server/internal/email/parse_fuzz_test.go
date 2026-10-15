package email_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// FuzzParse feeds arbitrary bytes to the inbound parser, which reads mail from anyone on the
// internet. It must never panic, and a message it accepts must keep the guarantees the rest of
// the product relies on: bounded thread IDs, header values without line breaks, lowercase
// addresses and a known authentication verdict.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"From: a@example.com\r\nSubject: hi\r\n\r\nbody\r\n",
		"From: =?UTF-8?Q?Jos=C3=A9?= <Jose@Example.COM>\r\nMessage-ID: <x@y>\r\nIn-Reply-To: <p@q>\r\nReferences: <r@s> <p@q>\r\nContent-Type: text/plain; charset=ISO-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nCa va tr=E8s bien.\r\n",
		"From: a@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/html\r\n\r\n<p>hi<img src=\"cid:i\"></p>\r\n--b\r\nContent-Type: image/png\r\nContent-ID: <i>\r\nContent-Transfer-Encoding: base64\r\n\r\niVBORw0KGgo=\r\n--b--\r\n",
		"From: a@example.com\r\nAuthentication-Results: mx.example; spf=pass; dkim=fail\r\nX-Original-To: Support@Desk.Example\r\n\r\n",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed), "mx.example")
	}
	f.Fuzz(func(t *testing.T, raw []byte, authServID string) {
		parsed, err := email.Parse(raw, authServID)
		if err != nil {
			return
		}
		if parsed.MessageID == "" || len(parsed.MessageID) > 255 {
			t.Fatalf("message id %q has length %d", parsed.MessageID, len(parsed.MessageID))
		}
		if len(parsed.InReplyTo) > 10 || len(parsed.References) > 50 {
			t.Fatalf("in-reply-to %d, references %d", len(parsed.InReplyTo), len(parsed.References))
		}
		for _, id := range append(append([]string{}, parsed.InReplyTo...), parsed.References...) {
			if len(id) > 255 {
				t.Fatalf("thread id of length %d", len(id))
			}
		}
		for name, value := range map[string]string{"subject": parsed.Subject, "from name": parsed.FromName} {
			if strings.ContainsAny(value, "\r\n") {
				t.Fatalf("%s %q contains a line break", name, value)
			}
		}
		for name, value := range map[string]string{"from": parsed.FromAddress, "envelope to": parsed.EnvelopeTo} {
			if value != strings.ToLower(value) {
				t.Fatalf("%s %q is not lowercase", name, value)
			}
		}
		switch parsed.Authentication {
		case email.AuthPass, email.AuthFail, email.AuthNone:
		default:
			t.Fatalf("authentication %q", parsed.Authentication)
		}
		if !utf8.ValidString(parsed.BodyHTML) || strings.Contains(parsed.BodyHTML, "\x00") {
			t.Fatalf("body html is not clean UTF-8")
		}
	})
}
