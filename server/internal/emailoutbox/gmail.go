package emailoutbox

import (
	"context"
	"errors"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// gmailSender sends through a connected Gmail account.
type gmailSender struct{ config google.Config }

func (g gmailSender) configured() bool                       { return g.config.Enabled() }
func (g gmailSender) keys() tokenseal.Keys                   { return g.config.Keys }
func (g gmailSender) grantContext(workspaceID string) string { return google.GrantContext(workspaceID) }

func (g gmailSender) accessToken(ctx context.Context, refresh string) (string, string, error) {
	return g.config.AccessToken(ctx, refresh)
}

func (g gmailSender) send(ctx context.Context, access string, message []byte) error {
	return g.config.Send(ctx, access, message)
}

func (g gmailSender) verdict(err error) mailboxVerdict {
	var apiErr *google.APIError
	switch {
	case errors.Is(err, google.ErrUnauthorized):
		return mailboxVerdict{kind: verdictUnauthorized}
	case errors.Is(err, google.ErrRevoked):
		return mailboxVerdict{kind: verdictBroken, code: domain.MailboxRevoked}
	case errors.As(err, &apiErr) && apiErr.ConnectionBroken():
		return mailboxVerdict{kind: verdictBroken, code: domain.MailboxPermission}
	case errors.As(err, &apiErr) && !apiErr.Temporary():
		return mailboxVerdict{kind: verdictFailed}
	default:
		return mailboxVerdict{kind: verdictRetry}
	}
}
