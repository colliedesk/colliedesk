package domain

import "time"

// MailboxConnection is the mailbox (a Gmail account) an email channel sends through. The grant itself
// never leaves the server; this is what settings show.
type MailboxConnection struct {
	// Provider is who holds the mailbox: MailboxGoogle.
	Provider string `json:"provider"`
	// Address is the account replies are sent from.
	Address string `json:"address"`
	// Status is "connected", or "failed" when the provider stopped honouring the grant: replies then fail
	// with Error until an admin reconnects. They never leave through another transport.
	Status      string     `json:"status"`
	ErrorCode   *string    `json:"error_code"`
	Error       *string    `json:"error"`
	FailedAt    *time.Time `json:"failed_at"`
	ConnectedAt time.Time  `json:"connected_at"`
}

// MailboxGoogle is the provider of a Gmail or Google Workspace mailbox.
const MailboxGoogle = "google"

// mailboxTransports is the transport (channel_outbox.transport) of the replies each provider sends.
var mailboxTransports = map[string]string{MailboxGoogle: "gmail"}

// MailboxTransport is the transport of a reply queued for a mailbox of the provider.
func MailboxTransport(provider string) string { return mailboxTransports[provider] }

// MailboxProviderOf is the provider whose mailbox sends replies of the transport; false for a transport
// that is not a mailbox (smtp).
func MailboxProviderOf(transport string) (string, bool) {
	for provider, sends := range mailboxTransports {
		if sends == transport {
			return provider, true
		}
	}
	return "", false
}

// Mailbox connection states and the reasons a connection fails.
const (
	MailboxConnected = "connected"
	MailboxFailed    = "failed"
	// MailboxRevoked: Google refuses the refresh token (access removed in the Google account, password
	// change, an expired testing-mode grant).
	MailboxRevoked = "revoked"
	// MailboxPermission: Gmail refuses to send for this grant (gmail.send withdrawn, account suspended).
	MailboxPermission = "permission"
)

// MailboxKeyNote is why a reply waits when this server's MAILBOX_TOKEN_KEYS cannot open the stored grant.
// It is about the server, not the grant, so the connection is not marked failed.
func MailboxKeyNote(provider string) string {
	return "This server cannot read the Gmail connection with its MAILBOX_TOKEN_KEYS; the reply is retried. Ask the administrator to check the keys, or reconnect Gmail in Settings."
}

// MailboxFailureText explains a failed connection to the admin and on every reply it stops.
func MailboxFailureText(provider, code string) string {
	switch code {
	case MailboxRevoked:
		return "Google no longer accepts the Gmail connection (access was removed or expired). Reconnect Gmail in Settings, then retry the reply."
	case MailboxPermission:
		return "Gmail refused to send for this connection. Reconnect Gmail in Settings and allow sending email, then retry the reply."
	default:
		return "The Gmail connection stopped working. Reconnect Gmail in Settings, then retry the reply."
	}
}

// MailboxNotConfiguredNote is the reason of a reply for a connected mailbox on a server that lost the
// provider's settings.
func MailboxNotConfiguredNote(provider string) string {
	return "Gmail sending is not configured on this server. Ask the administrator to set GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET and MAILBOX_TOKEN_KEYS."
}

// MailboxDisconnectedNote is the reason of a reply accepted for a mailbox whose connection was removed,
// or replaced by another provider's, before it was sent.
func MailboxDisconnectedNote(provider string) string {
	return "Gmail was disconnected before this reply was sent. Retry to send it the way the workspace sends now."
}
