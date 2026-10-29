package channelemail

import (
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	mailpkg "github.com/colliedesk/colliedesk/server/internal/email"
)

// GoogleForwardingSender is the address Gmail and Google Workspace send their forwarding confirmation from.
const GoogleForwardingSender = "forwarding-noreply@google.com"

// ForwardingConfirmation is what a mail provider's confirmation of a new forwarding address carries: the
// code a person can type at the provider, and the link that confirms, when it can be trusted.
type ForwardingConfirmation struct {
	Code       string
	ConfirmURL string
}

var (
	confirmationCodeInBody    = regexp.MustCompile(`(?i)confirmation code:\s*([0-9]{6,12})\b`)
	confirmationCodeInSubject = regexp.MustCompile(`\(#([0-9]{6,12})\)`)
	links                     = regexp.MustCompile(`https?://[^\s<>"]+`)
	// confirmHosts are Google's hosts of the confirmation link; any other host is never offered as a link.
	confirmHosts = map[string]bool{"mail.google.com": true, "mail-settings.google.com": true}
)

// ParseForwardingConfirmation recognises Gmail's "Forwarding Confirmation" email by its sender and subject
// and reads its code. The confirmation link is returned only when it is https on one of Google's
// confirmation hosts and the message passed the sender check of the trusted receiving server
// (authentication, the parser's verdict on the trusted Authentication-Results): anyone can send mail
// with this From line, and a forged one must never become a button.
func ParseForwardingConfirmation(from, subject, body, authentication string) (ForwardingConfirmation, bool) {
	sender := from
	if parsed, err := mail.ParseAddress(from); err == nil {
		sender = parsed.Address
	}
	if !strings.EqualFold(strings.TrimSpace(sender), GoogleForwardingSender) || !strings.Contains(strings.ToLower(subject), "forwarding confirmation") {
		return ForwardingConfirmation{}, false
	}
	var found ForwardingConfirmation
	if match := confirmationCodeInBody.FindStringSubmatch(body); match != nil {
		found.Code = match[1]
	} else if match := confirmationCodeInSubject.FindStringSubmatch(subject); match != nil {
		found.Code = match[1]
	}
	if found.Code == "" {
		return ForwardingConfirmation{}, false
	}
	if authentication == mailpkg.AuthPass {
		for _, link := range links.FindAllString(body, -1) {
			if found.ConfirmURL = confirmLink(link); found.ConfirmURL != "" {
				break
			}
		}
	}
	return found, true
}

// confirmLink is link when it is an https address on a Google confirmation host whose path confirms
// (/mail/vf-…), else "". The same email also carries a link that cancels (/mail/uf-…): never offered.
func confirmLink(link string) string {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !confirmHosts[strings.ToLower(parsed.Hostname())] ||
		!strings.HasPrefix(parsed.EscapedPath(), "/mail/vf-") {
		return ""
	}
	return link
}
