package email

import (
	"mime"
	"net/mail"
	"regexp"
	"strings"
)

// Drop reasons for mail that must never open or reopen a conversation.
const (
	DropAutoSubmitted = "auto_submitted"
	DropBulk          = "bulk"
	DropMailerDaemon  = "mailer_daemon"
	DropOwnMessage    = "own_message"
	DropOwnAddress    = "own_address"
)

var (
	resultPattern     = regexp.MustCompile(`\b(spf|dkim|dmarc)\s*=\s*([a-z]+)`)
	replyTokenPattern = regexp.MustCompile(`(?:\+r-|^reply\+)([a-f0-9]{32})@`)
)

// authentication reads the topmost Authentication-Results header stamped by the trusted receiving
// server (its authserv-id). Any other header may have been written by the sender and is ignored.
// DMARC decides when present; otherwise any DKIM or SPF pass is a pass.
func authentication(headers []string, trustedAuthServID string) string {
	if trustedAuthServID == "" {
		return AuthNone
	}
	var stamp string
	for _, header := range headers {
		authServ, _, _ := strings.Cut(header, ";")
		fields := strings.Fields(authServ)
		if len(fields) > 0 && strings.EqualFold(fields[0], trustedAuthServID) {
			stamp = header
			break
		}
	}
	if stamp == "" {
		return AuthNone
	}
	results := map[string][]string{}
	for _, match := range resultPattern.FindAllStringSubmatch(strings.ToLower(stamp), -1) {
		results[match[1]] = append(results[match[1]], match[2])
	}
	has := func(method string, values ...string) bool {
		for _, got := range results[method] {
			for _, want := range values {
				if got == want {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("dmarc", "pass"):
		return AuthPass
	case has("dmarc", "fail"):
		return AuthFail
	case has("dkim", "pass") || has("spf", "pass"):
		return AuthPass
	case has("dkim", "fail") || has("spf", "fail", "softfail"):
		return AuthFail
	default:
		return AuthNone
	}
}

// dropReason flags automatic mail (RFC 3834 auto-replies, bulk and list mail, delivery reports).
func dropReason(header mail.Header, from string) string {
	local := from
	if at := strings.LastIndex(from, "@"); at >= 0 {
		local = from[:at]
	}
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if local == "mailer-daemon" || local == "postmaster" ||
		(mediaType == "multipart/report" && strings.EqualFold(params["report-type"], "delivery-status")) {
		return DropMailerDaemon
	}
	autoSubmitted := strings.ToLower(strings.TrimSpace(header.Get("Auto-Submitted")))
	precedence := strings.ToLower(strings.TrimSpace(header.Get("Precedence")))
	if (autoSubmitted != "" && autoSubmitted != "no") || header.Get("X-Autoreply") != "" ||
		header.Get("X-Autorespond") != "" || precedence == "auto_reply" {
		return DropAutoSubmitted
	}
	// List headers alone are not bulk: a Google Group or list alias in front of support@ stamps
	// List-Id and Precedence: list on every real customer message. They count only with a bulk-sender
	// signal (one-click unsubscribe, an ESP feedback id).
	listSignal := header.Get("List-Id") != "" || precedence == "list"
	bulkSender := header.Get("List-Unsubscribe-Post") != "" || header.Get("Feedback-Id") != ""
	if precedence == "bulk" || precedence == "junk" || (listSignal && bulkSender) {
		return DropBulk
	}
	return ""
}

// ReplyToken returns the conversation token from a plus address such as support+r-TOKEN@example.com.
// Tokens are lowercase hex because some relays lowercase the local part.
func ReplyToken(recipients []string) string {
	for _, recipient := range recipients {
		if match := replyTokenPattern.FindStringSubmatch(strings.ToLower(recipient)); match != nil {
			return match[1]
		}
	}
	return ""
}

// PlatformReplyToken returns the token of a reply+<token>@<domain> recipient on the platform sending
// domain, used when a workspace sends without a verified domain.
func PlatformReplyToken(recipients []string, domain string) string {
	if domain == "" {
		return ""
	}
	suffix := "@" + strings.ToLower(domain)
	for _, recipient := range recipients {
		recipient = strings.ToLower(recipient)
		if strings.HasSuffix(recipient, suffix) && strings.HasPrefix(recipient, "reply+") {
			if match := replyTokenPattern.FindStringSubmatch(recipient); match != nil {
				return match[1]
			}
		}
	}
	return ""
}

// PlusAddress inserts a reply token into an address: support@example.com -> support+r-TOKEN@example.com.
func PlusAddress(address, token string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 || token == "" {
		return address
	}
	return address[:at] + "+r-" + token + address[at:]
}

// SameMailbox reports whether address reaches the mailbox of mailbox: the same domain and the same
// local part once a "+tag" is removed, compared without case. support+r-token@example.com is the
// support address support@example.com.
func SameMailbox(address, mailbox string) bool {
	base := func(value string) (string, bool) {
		at := strings.LastIndex(value, "@")
		if at < 1 {
			return "", false
		}
		local, _, _ := strings.Cut(value[:at], "+")
		return strings.ToLower(local + value[at:]), local != ""
	}
	left, ok := base(address)
	right, okMailbox := base(mailbox)
	return ok && okMailbox && left == right
}

// Domain returns the part after the last @.
func Domain(address string) string {
	return address[strings.LastIndex(address, "@")+1:]
}
