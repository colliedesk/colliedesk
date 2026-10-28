package emailoutbox

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const (
	// serverConfigDeferral is how often a reply waiting for this server's Microsoft credentials is tried.
	serverConfigDeferral = 15 * time.Minute
	// keepAliveAge is how long a Microsoft 365 grant may go unrenewed before the keep-alive refreshes it:
	// Microsoft expires a refresh token unused for 90 days (refresh-tokens).
	keepAliveAge = 7 * 24 * time.Hour
	// keepAliveBatch caps the grants renewed in one round.
	keepAliveBatch = 100
)

// microsoftSender sends through a connected Microsoft 365 or Outlook.com mailbox.
type microsoftSender struct {
	config microsoft.Config
	now    func() time.Time
}

func (m microsoftSender) configured() bool     { return m.config.Enabled() }
func (m microsoftSender) keys() tokenseal.Keys { return m.config.Keys }
func (m microsoftSender) grantContext(workspaceID string) string {
	return microsoft.GrantContext(workspaceID)
}

func (m microsoftSender) accessToken(ctx context.Context, refresh string) (string, string, error) {
	return m.config.AccessToken(ctx, refresh)
}

func (m microsoftSender) send(ctx context.Context, access string, message []byte) error {
	return m.config.Send(ctx, access, message)
}

func (m microsoftSender) verdict(err error) mailboxVerdict {
	var apiErr *microsoft.APIError
	switch {
	case errors.Is(err, microsoft.ErrUnauthorized):
		return mailboxVerdict{kind: verdictUnauthorized}
	case errors.Is(err, microsoft.ErrRevoked):
		return mailboxVerdict{kind: verdictBroken, code: domain.MailboxRevoked}
	case errors.Is(err, microsoft.ErrClientConfig):
		// The server's client secret, not the grant: the reply waits for the operator, the connection stays.
		return mailboxVerdict{kind: verdictDefer, code: domain.MailboxServerConfigCode, until: m.now().Add(serverConfigDeferral), note: domain.MailboxServerConfigNote}
	case !errors.As(err, &apiErr):
		return mailboxVerdict{kind: verdictRetry}
	case apiErr.MailboxUnavailable():
		return mailboxVerdict{kind: verdictBroken, code: domain.MailboxUnavailable}
	case apiErr.TooLarge():
		// Never sent without its files: the reply fails, keeps them, and a person decides.
		return mailboxVerdict{kind: verdictFailed, note: domain.MailboxTooLargeNote}
	case apiErr.Temporary() && apiErr.RetryAfter > 0:
		return mailboxVerdict{kind: verdictDefer, until: m.now().Add(apiErr.RetryAfter)}
	case apiErr.Temporary():
		return mailboxVerdict{kind: verdictRetry}
	case apiErr.PermissionDenied():
		return mailboxVerdict{kind: verdictBroken, code: domain.MailboxPermission}
	case apiErr.Status >= http.StatusBadRequest:
		return mailboxVerdict{kind: verdictFailed}
	default:
		return mailboxVerdict{kind: verdictRetry}
	}
}

// KeepAlive refreshes the Microsoft 365 grants nothing renewed for a week, in every workspace, and returns
// how many it renewed. Microsoft expires a refresh token unused for 90 days, so a quiet channel would stop
// without anyone noticing; a grant Microsoft refuses is marked failed now, before a customer waits for a
// reply. A refusal of this server's credentials only logs: the connections are fine.
func (w *Worker) KeepAlive(ctx context.Context) (int, error) {
	sender, ok := w.mailboxes()[domain.MailboxMicrosoft]
	if !ok || !sender.configured() {
		return 0, nil
	}
	now := w.options.Now()
	due, err := w.store.MailboxesToKeepAlive(ctx, domain.MailboxMicrosoft, now.Add(-keepAliveAge), keepAliveBatch)
	if err != nil {
		return 0, err
	}
	renewed := 0
	for _, ref := range due {
		grant, err := w.store.MailboxGrant(ctx, ref.WorkspaceID, ref.ChannelID)
		if err != nil {
			w.options.Logger.Warn("read a mailbox grant to keep it alive", "workspace", ref.WorkspaceID, "error", err)
			continue
		}
		if grant == nil || grant.Provider != domain.MailboxMicrosoft {
			continue
		}
		refresh, err := sender.keys().Open(grant.KeyID, grant.Sealed, sender.grantContext(ref.WorkspaceID))
		if err != nil {
			w.options.Logger.Warn("mailbox grant does not open with this server's keys", "provider", domain.MailboxMicrosoft, "workspace", ref.WorkspaceID, "key", grant.KeyID)
			continue
		}
		_, rotated, err := sender.accessToken(ctx, string(refresh))
		if err != nil {
			if verdict := sender.verdict(err); verdict.kind == verdictBroken {
				w.options.Logger.Warn("mailbox connection failed", "provider", domain.MailboxMicrosoft, "workspace", ref.WorkspaceID, "reason", verdict.code)
				if err := w.store.MarkMailboxFailed(ctx, ref.WorkspaceID, *grant, verdict.code, w.options.Now()); err != nil {
					w.options.Logger.Error("mark mailbox connection failed", "provider", domain.MailboxMicrosoft, "workspace", ref.WorkspaceID, "error", err)
				}
			} else {
				w.options.Logger.Warn("keep a mailbox grant alive", "provider", domain.MailboxMicrosoft, "workspace", ref.WorkspaceID, "error", err)
			}
			continue
		}
		// Storing the same token again still marks the grant renewed, so it is not asked again for a week.
		if rotated == "" {
			rotated = string(refresh)
		}
		if err := w.storeGrant(ctx, sender, ref.WorkspaceID, grant, []byte(rotated)); err != nil {
			w.options.Logger.Warn("store a renewed mailbox grant", "provider", domain.MailboxMicrosoft, "workspace", ref.WorkspaceID, "error", err)
			continue
		}
		renewed++
	}
	return renewed, nil
}

// RunKeepAlive runs KeepAlive at once and then every interval until the context ends.
func (w *Worker) RunKeepAlive(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.KeepAlive(ctx); err != nil && ctx.Err() == nil {
			w.options.Logger.Warn("mailbox keep-alive", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
