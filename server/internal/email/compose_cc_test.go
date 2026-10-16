package email_test

import (
	"fmt"
	"net/mail"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

func parseComposed(t *testing.T, message email.Outgoing) *mail.Message {
	t.Helper()
	raw, err := email.Compose(message)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestComposeWritesOneCcHeaderWithEveryCopiedAddress(t *testing.T) {
	message := outgoing()
	message.Cc = []string{"boss@customer.example", "ops+billing@customer.example"}
	parsed := parseComposed(t, message)
	list, err := parsed.Header.AddressList("Cc")
	if err != nil || len(list) != 2 || list[0].Address != "boss@customer.example" || list[1].Address != "ops+billing@customer.example" {
		t.Fatalf("Cc = %v, err=%v", list, err)
	}
	if got := parsed.Header["Bcc"]; len(got) != 0 {
		t.Fatalf("Bcc header present: %v", got)
	}
	if to, _ := parsed.Header.AddressList("To"); len(to) != 1 || to[0].Address != "ada@customer.example" {
		t.Fatalf("To must stay the contact only: %v", to)
	}
}

func TestComposeWithoutCcWritesNoCcHeader(t *testing.T) {
	if got := parseComposed(t, outgoing()).Header["Cc"]; len(got) != 0 {
		t.Fatalf("Cc = %v", got)
	}
}

func TestComposeRefusesCcThatCouldInjectHeadersOrIsNotAPlainAddress(t *testing.T) {
	for _, bad := range []string{
		"a@customer.example\r\nBcc: evil@attacker.example",
		"Boss <boss@customer.example>",
		"not-an-address",
		"a@customer.example, b@customer.example",
		"",
	} {
		message := outgoing()
		message.Cc = []string{bad}
		if raw, err := email.Compose(message); err == nil {
			t.Errorf("Cc %q composed:\n%s", bad, raw)
		}
	}
}

func TestComposeEncodesInternationalizedCcLocalPartAsGiven(t *testing.T) {
	message := outgoing()
	message.Cc = []string{"quinn.o'neil@customer.example"}
	parsed := parseComposed(t, message)
	if list, err := parsed.Header.AddressList("Cc"); err != nil || len(list) != 1 || list[0].Address != "quinn.o'neil@customer.example" {
		t.Fatalf("Cc = %v, err=%v", list, err)
	}
}

func TestComposeKeepsCcHeaderOnMultipartMessages(t *testing.T) {
	message := outgoing(email.File{Filename: "a.txt", ContentType: "text/plain", Data: []byte("a")})
	message.Cc = []string{"boss@customer.example"}
	parsed := parseComposed(t, message)
	if list, err := parsed.Header.AddressList("Cc"); err != nil || len(list) != 1 {
		t.Fatalf("Cc = %v, err=%v", list, err)
	}
}

func TestParseReadsCcAddressesLowercasedAndDeduplicated(t *testing.T) {
	parsed, err := email.Parse(crlf(`From: Ada <ada@customer.example>
To: support@acme.example, Boss <Boss@Customer.example>
Cc: "Ops, Team" <ops@customer.example>, boss@customer.example, =?UTF-8?Q?Jos=C3=A9?= <jose@customer.example>
Cc: late@customer.example
Subject: x
Message-ID: <cc-1@customer.example>

Hi.
`), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(parsed.Cc, ","); got != "ops@customer.example,boss@customer.example,jose@customer.example,late@customer.example" {
		t.Fatalf("Cc = %q", got)
	}
}

func TestParseIgnoresAnUnreadableCcHeader(t *testing.T) {
	parsed, err := email.Parse(crlf("From: a@customer.example\nCc: <<broken\nSubject: x\nMessage-ID: <cc-2@customer.example>\n\nHi.\n"), "")
	if err != nil || len(parsed.Cc) != 0 {
		t.Fatalf("Cc = %v, err=%v", parsed.Cc, err)
	}
}

func TestParseKeepsAtMostFiftyCcAddresses(t *testing.T) {
	var list []string
	for i := 0; i < 80; i++ {
		list = append(list, fmt.Sprintf("p%d@customer.example", i))
	}
	parsed, err := email.Parse(crlf("From: a@customer.example\nCc: "+strings.Join(list, ", ")+"\nSubject: x\nMessage-ID: <cc-3@customer.example>\n\nHi.\n"), "")
	if err != nil || len(parsed.Cc) != 50 || parsed.Cc[0] != "p0@customer.example" {
		t.Fatalf("Cc has %d addresses, err=%v", len(parsed.Cc), err)
	}
}
