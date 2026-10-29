package channelemail_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
)

// The bodies below are SYNTHETIC: written from Google's documented "Forwarding Confirmation" email (Gmail
// Help, "Automatically forward Gmail messages to another account") and its public descriptions, not
// copied from a received message. Addresses, the code and the link tokens are made up.
const syntheticGmailSubject = "(#482913557) Gmail Forwarding Confirmation - Receive Mail from support@acme.example"

const syntheticGmailBody = `support@acme.example has requested to automatically forward mail to your email
address support@acme.colliedesk.com.
Confirmation code: 482913557

To allow support@acme.example to automatically forward mail to your address,
please click the link below to confirm the request:

https://mail-settings.google.com/mail/vf-%5BANGjdJ9synthetic%5D-c3VwcG9ydEBhY21l

If you click the link and it appears to be broken, please copy and paste it
into a new browser window. If you aren't able to access the link, you can
send the confirmation code 482913557 to support@acme.example.

Thanks for using Gmail!

Sincerely,

The Gmail Team

If you do not approve of this request, no further action is required.
support@acme.example cannot automatically forward messages to your email address
unless you confirm the request by clicking the link above. If you
accidentally clicked the link, but you do not want to allow support@acme.example
to automatically forward messages to your address, click this link to
cancel this verification:
https://mail-settings.google.com/mail/uf-%5BANGjdJ9synthetic%5D-c3VwcG9ydEBhY21l
`

const googleSender = "forwarding-noreply@google.com"

func TestForwardingConfirmationSyntheticGmailReadsCodeAndConfirmLink(t *testing.T) {
	got, ok := channelemail.ParseForwardingConfirmation(googleSender, syntheticGmailSubject, syntheticGmailBody, "pass")
	if !ok {
		t.Fatal("a Gmail forwarding confirmation was not recognised")
	}
	if got.Code != "482913557" {
		t.Fatalf("code = %q, want 482913557", got.Code)
	}
	// The first link confirms; the second one, further down, cancels and must never be offered.
	if got.ConfirmURL != "https://mail-settings.google.com/mail/vf-%5BANGjdJ9synthetic%5D-c3VwcG9ydEBhY21l" {
		t.Fatalf("confirm url = %q", got.ConfirmURL)
	}
}

func TestForwardingConfirmationSyntheticOlderFormatOnMailGoogleCom(t *testing.T) {
	body := strings.ReplaceAll(syntheticGmailBody, "https://mail-settings.google.com/mail/vf-", "https://mail.google.com/mail/vf-")
	got, ok := channelemail.ParseForwardingConfirmation("Gmail Team <FORWARDING-NOREPLY@GOOGLE.COM>", syntheticGmailSubject, body, "pass")
	if !ok || got.ConfirmURL != "https://mail.google.com/mail/vf-%5BANGjdJ9synthetic%5D-c3VwcG9ydEBhY21l" {
		t.Fatalf("got %+v ok=%v, want the mail.google.com link", got, ok)
	}
}

// Anyone can mail a forwarding address with From: forwarding-noreply@google.com. Without a passing
// sender check from the trusted receiving server the code is shown, never a link to click.
func TestForwardingConfirmationForgedSenderGivesTheCodeOnly(t *testing.T) {
	for _, authentication := range []string{"fail", "none", ""} {
		got, ok := channelemail.ParseForwardingConfirmation(googleSender, syntheticGmailSubject, syntheticGmailBody, authentication)
		if !ok || got.Code != "482913557" {
			t.Fatalf("authentication %q: got %+v ok=%v, want the code", authentication, got, ok)
		}
		if got.ConfirmURL != "" {
			t.Fatalf("authentication %q: confirm url %q offered for an unauthenticated sender", authentication, got.ConfirmURL)
		}
	}
}

// A link that is not Google's confirmation host is dropped even from an authenticated message.
func TestForwardingConfirmationRefusesALinkOutsideGoogleHosts(t *testing.T) {
	for _, link := range []string{
		"https://mail-settings.google.com.evil.example/mail/vf-x",
		"https://evil.example/mail.google.com/mail/vf-x",
		"http://mail-settings.google.com/mail/vf-x",
		"https://accounts.google.com/mail/vf-x",
		"https://user@mail.google.com.evil.example/vf-x",
		"javascript:alert(1)//mail.google.com",
	} {
		body := strings.Replace(syntheticGmailBody, "https://mail-settings.google.com/mail/vf-%5BANGjdJ9synthetic%5D-c3VwcG9ydEBhY21l", link, 1)
		got, ok := channelemail.ParseForwardingConfirmation(googleSender, syntheticGmailSubject, body, "pass")
		if !ok || got.Code != "482913557" {
			t.Fatalf("%s: got %+v ok=%v, want the code", link, got, ok)
		}
		if got.ConfirmURL != "" {
			t.Fatalf("%s: confirm url %q offered", link, got.ConfirmURL)
		}
	}
}

func TestForwardingConfirmationIgnoresOtherMail(t *testing.T) {
	cases := map[string][3]string{
		"another sender":   {"forwarding-noreply@google.com.evil.example", syntheticGmailSubject, syntheticGmailBody},
		"a lookalike name": {"Gmail Team <noreply@evil.example>", syntheticGmailSubject, syntheticGmailBody},
		"another subject":  {googleSender, "Your Google Account was signed in", syntheticGmailBody},
		"no code at all":   {googleSender, "Gmail Forwarding Confirmation", "There is nothing to confirm here."},
	}
	for name, mail := range cases {
		if got, ok := channelemail.ParseForwardingConfirmation(mail[0], mail[1], mail[2], "pass"); ok {
			t.Fatalf("%s: recognised as a confirmation: %+v", name, got)
		}
	}
}

// The code is also in the subject, "(#…)": a body Google words differently still gives it.
func TestForwardingConfirmationReadsTheCodeFromTheSubject(t *testing.T) {
	got, ok := channelemail.ParseForwardingConfirmation(googleSender, syntheticGmailSubject, "Confirm with the link below.\nhttps://mail-settings.google.com/mail/vf-abc\n", "pass")
	if !ok || got.Code != "482913557" || got.ConfirmURL != "https://mail-settings.google.com/mail/vf-abc" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}
