package email_test

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// A sender name may be 100 characters of any script. Encoded, such a name is far longer than a mail
// header line may be (998 bytes, RFC 5322 section 2.1.1), so From and Reply-To are folded like the
// subject, and the receiving side still reads the exact name back.
func TestLongNonLatinSenderNamesAreFoldedAndReadBackExactly(t *testing.T) {
	names := map[string]string{
		"CJK":   strings.Repeat("客服", 50),
		"emoji": strings.Repeat("📮", 100),
	}
	for script, name := range names {
		t.Run(script, func(t *testing.T) {
			message := outgoing()
			message.FromName = name + " via Colliedesk"
			message.ReplyTo, message.ReplyToName = "reply+token@mail.colliedesk.example", name
			raw, err := email.Compose(message)
			if err != nil {
				t.Fatal(err)
			}
			head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
			for _, line := range strings.Split(head, "\r\n") {
				if len(line) > 998 {
					t.Fatalf("header line of %d bytes, the limit is 998: %.80q…", len(line), line)
				}
			}
			parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			from, err := mail.ParseAddress(parsed.Header.Get("From"))
			if err != nil || from.Name != name+" via Colliedesk" || from.Address != "support@acme.example" {
				t.Fatalf("From read back as %+v (err %v)", from, err)
			}
			replyTo, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
			if err != nil || replyTo.Name != name || replyTo.Address != "reply+token@mail.colliedesk.example" {
				t.Fatalf("Reply-To read back as %+v (err %v)", replyTo, err)
			}
		})
	}
}
