package domain

import "time"

// MailboxConnection is the mailbox (a Gmail account, a Microsoft 365 or Outlook.com mailbox) an email
// channel sends through. The grant itself never leaves the server; this is what settings show.
type MailboxConnection struct {
	// Provider is who holds the mailbox: MailboxGoogle or MailboxMicrosoft.
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

// Mailbox providers.
const (
	// MailboxGoogle is the provider of a Gmail or Google Workspace mailbox.
	MailboxGoogle = "google"
	// MailboxMicrosoft is the provider of a Microsoft 365 (Exchange Online) or Outlook.com mailbox.
	MailboxMicrosoft = "microsoft"
)

// mailboxTransports is the transport (channel_outbox.transport) of the replies each provider sends.
var mailboxTransports = map[string]string{MailboxGoogle: "gmail", MailboxMicrosoft: "microsoft"}

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
	// MailboxRevoked: the provider refuses the refresh token (access removed in the account, password
	// change or reset, an expired grant, a sign-in policy of the organization that now needs the person).
	MailboxRevoked = "revoked"
	// MailboxPermission: the provider refuses to send for this grant (gmail.send withdrawn, account
	// suspended, sending from apps blocked).
	MailboxPermission = "permission"
	// MailboxUnavailable: the Microsoft account has no Exchange Online mailbox (any more), for example
	// because its licence was removed.
	MailboxUnavailable = "mailbox_unavailable"
)

// MailboxServerConfigCode is the code of a reply waiting because the provider refused this server's own
// app credentials: the connection is fine, and the operator must fix the server.
const MailboxServerConfigCode = "mailbox_server_config"

// MailboxKeyNote is why a reply waits when this server's MAILBOX_TOKEN_KEYS cannot open the stored grant.
// Screens show it, so it names no server variable; docs/self-host does.
// It is about the server, not the grant, so the connection is not marked failed.
func MailboxKeyNote(provider string) string {
	if provider == MailboxMicrosoft {
		return "This server cannot read the Microsoft 365 connection with its key for stored connections; the reply is retried. Reconnect Microsoft 365 in Settings, or ask whoever runs it to check that key."
	}
	return "This server cannot read the Gmail connection with its key for stored connections; the reply is retried. Reconnect Gmail in Settings, or ask whoever runs it to check that key."
}

// MailboxFailureText explains a failed connection to the admin and on every reply it stops.
func MailboxFailureText(provider, code string) string {
	if provider == MailboxMicrosoft {
		switch code {
		case MailboxRevoked:
			return "Microsoft no longer accepts the Microsoft 365 connection (access was removed, the password was reset, or a sign-in policy now blocks it). Reconnect Microsoft 365 in Settings, then retry the reply."
		case MailboxPermission:
			return "Microsoft 365 refused to send from this mailbox. Reconnect Microsoft 365 in Settings, then retry the reply."
		case MailboxUnavailable:
			return "This Microsoft account has no Exchange Online mailbox any more. Reconnect Microsoft 365 in Settings, then retry the reply."
		default:
			return "The Microsoft 365 connection stopped working. Reconnect Microsoft 365 in Settings, then retry the reply."
		}
	}
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
	if provider == MailboxMicrosoft {
		return "Microsoft 365 sending is not set up on this server: ask whoever runs it."
	}
	return "Gmail sending is not set up on this server: ask whoever runs it."
}

// MailboxSwitchedOffNote is the reason of a reply waiting because the operator switched its provider off.
func MailboxSwitchedOffNote(provider string) string {
	label := "Gmail"
	if provider == MailboxMicrosoft {
		label = "Microsoft 365"
	}
	return label + " sending is turned off on this server: this reply waits and is sent once it is on again."
}

// MailboxDisconnectedNote is the reason of a reply accepted for a mailbox whose connection was removed,
// or replaced by another provider's, before it was sent.
func MailboxDisconnectedNote(provider string) string {
	if provider == MailboxMicrosoft {
		return "Microsoft 365 was disconnected before this reply was sent. Retry to send it the way the workspace sends now."
	}
	return "Gmail was disconnected before this reply was sent. Retry to send it the way the workspace sends now."
}

// MailboxServerConfigNote is why a reply for a Microsoft 365 mailbox waits when Microsoft refused this
// server's client secret. It is retried until the operator fixes it; reconnecting does not help.
const MailboxServerConfigNote = "Waiting: Microsoft refused this server's app credentials, so whoever runs this server must replace its Microsoft app secret; the reply is sent once it works. Reconnecting does not help."

// MailboxTooLargeNote is why a reply fails when Microsoft 365 refuses its size. Its files are kept.
const MailboxTooLargeNote = "The reply is too large to send through Microsoft 365. Remove a file and retry."
