package emailoutbox

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// mailboxProvider is what sending through a connected mailbox needs of its provider (Gmail, Microsoft 365).
type mailboxProvider interface {
	// configured reports whether this server has the provider's settings.
	configured() bool
	keys() tokenseal.Keys
	grantContext(workspaceID string) string
	// accessToken gets an access token; rotated is a refresh token the provider replaced, "" for none.
	accessToken(ctx context.Context, refresh string) (access, rotated string, err error)
	send(ctx context.Context, access string, message []byte) error
	// verdict says what an error of accessToken or send means for the reply and the connection.
	verdict(err error) mailboxVerdict
}

// mailboxVerdict is what a provider's error means.
type mailboxVerdict struct {
	kind verdictKind
	// code is the connection's error code of verdictBroken, or the delivery's code of verdictDefer.
	code string
	// until is when a verdictDefer reply is tried again.
	until time.Time
	// note replaces the error's text as the reply's reason, when set.
	note string
}

type verdictKind int

const (
	// verdictRetry: temporary; the reply is retried with the outbox's backoff.
	verdictRetry verdictKind = iota
	// verdictUnauthorized: the access token was refused; refresh it and send once more.
	verdictUnauthorized
	// verdictBroken: the grant no longer sends; the connection is marked failed with code.
	verdictBroken
	// verdictFailed: this reply cannot be sent this way; it fails, the connection stays.
	verdictFailed
	// verdictDefer: the provider asked to wait; the reply is tried again at until.
	verdictDefer
)

// switchedOffWait is how long a reply waits before it looks again whether its provider is on.
const switchedOffWait = 10 * time.Minute

// mailboxes are the providers this worker can send through, by provider name.
func (w *Worker) mailboxes() map[string]mailboxProvider {
	providers := map[string]mailboxProvider{}
	if w.options.Google != nil {
		providers[domain.MailboxGoogle] = gmailSender{config: *w.options.Google}
	}
	if w.options.Microsoft != nil {
		providers[domain.MailboxMicrosoft] = microsoftSender{config: *w.options.Microsoft, now: w.options.Now}
	}
	return providers
}

// deliverMailbox sends a reply accepted for a mailbox of the provider from the connected account. It
// never hands the reply to SMTP: a removed, replaced or broken connection fails the reply with the
// reason, and only a retry (which chooses the transport again) sends it another way.
func (w *Worker) deliverMailbox(ctx context.Context, item store.Delivery, provider string) channels.SendResult {
	mailbox := item.Mailbox
	switch {
	case mailbox == nil || mailbox.Grant.Provider != provider:
		return channels.Failed(domain.MailboxDisconnectedNote(provider))
	case mailbox.Status == domain.MailboxFailed:
		return channels.Failed(domain.MailboxFailureText(provider, mailbox.ErrorCode))
	}
	sender, ok := w.mailboxes()[provider]
	if ok && sender.configured() && w.options.SwitchedOff[provider] {
		// Not an attempt: the reply waits for the operator, not for the provider.
		waits := channels.Defer(w.options.Now().Add(switchedOffWait), domain.MailboxSwitchedOffNote(provider))
		waits.Code = domain.MailboxSwitchedOffCode
		return waits
	}
	if !ok || !sender.configured() {
		return channels.Failed(domain.MailboxNotConfiguredNote(provider))
	}
	// The grant opens with the context of the provider that issued it, which the row names.
	refresh, err := sender.keys().Open(mailbox.Grant.KeyID, mailbox.Grant.Sealed, sender.grantContext(item.WorkspaceID))
	if err != nil {
		// This server's keys, not the grant: a replica with the right MAILBOX_TOKEN_KEYS sends it, so the
		// connection stays as it is and the reply waits like any temporary failure.
		w.options.Logger.Warn("mailbox grant does not open with this server's keys", "provider", provider, "workspace", item.WorkspaceID, "key", mailbox.Grant.KeyID)
		return w.retry(ctx, item, errors.New(domain.MailboxKeyNote(provider)))
	}
	if mailbox.Grant.KeyID != sender.keys().Current() {
		w.resealGrant(ctx, sender, item, refresh)
	}
	files, err := w.loadFiles(ctx, item)
	if errors.Is(err, errFileMissing) {
		return channels.Failed(truncate(err.Error()))
	}
	if err != nil {
		return w.retry(ctx, item, err)
	}
	copies, unusable := usableCopies(item.CC)
	message, err := w.compose(item, mail.Address{Name: item.SenderName, Address: mailbox.Address}, copies, files)
	if err != nil {
		return channels.Failed(truncate(err.Error()))
	}
	err = w.sendThrough(ctx, sender, item, string(refresh), message)
	if err == nil {
		return channels.Sent(truncate(missedCopies(unusable)))
	}
	verdict := sender.verdict(err)
	reason := err.Error()
	if verdict.note != "" {
		reason = verdict.note
	}
	switch verdict.kind {
	case verdictBroken:
		return w.breakConnection(ctx, item, provider, verdict.code)
	case verdictFailed:
		w.options.Logger.Warn("mailbox delivery failed", "provider", provider, "workspace", item.WorkspaceID, "message", item.MessageID, "error", err)
		return channels.Failed(truncate(reason))
	case verdictDefer:
		if verdict.code == domain.MailboxServerConfigCode {
			w.options.Logger.Error("the mailbox provider refused this server's credentials", "provider", provider, "workspace", item.WorkspaceID, "error", err)
		}
		result := channels.Defer(verdict.until, truncate(reason))
		result.Code = verdict.code
		return result
	default:
		return w.retry(ctx, item, errors.New(reason))
	}
}

// sendThrough gets an access token and sends; a refused access token is refreshed and tried once more.
func (w *Worker) sendThrough(ctx context.Context, sender mailboxProvider, item store.Delivery, refresh string, message []byte) error {
	var err error
	for range 2 {
		var access, rotated string
		access, rotated, err = sender.accessToken(ctx, refresh)
		if err != nil {
			return err
		}
		if rotated != "" {
			w.keepRotatedGrant(ctx, sender, item, rotated)
			refresh = rotated
		}
		err = sender.send(ctx, access, message)
		if err == nil || sender.verdict(err).kind != verdictUnauthorized {
			return err
		}
	}
	return fmt.Errorf("%w twice in a row", err)
}

// keepRotatedGrant stores a refresh token the provider replaced. Failing to store it only logs: the old
// one keeps working until the provider says otherwise, which then shows as a broken connection.
func (w *Worker) keepRotatedGrant(ctx context.Context, sender mailboxProvider, item store.Delivery, rotated string) {
	if err := w.storeGrant(ctx, sender, item.WorkspaceID, &item.Mailbox.Grant, []byte(rotated)); err != nil {
		w.options.Logger.Warn("keep rotated mailbox grant", "provider", item.Mailbox.Grant.Provider, "workspace", item.WorkspaceID, "error", err)
	}
}

// resealGrant seals a grant opened with an older key again with the first key, so the older key can be
// removed from MAILBOX_TOKEN_KEYS. A failure only logs; the reply goes on with the grant it opened.
func (w *Worker) resealGrant(ctx context.Context, sender mailboxProvider, item store.Delivery, refresh []byte) {
	if err := w.storeGrant(ctx, sender, item.WorkspaceID, &item.Mailbox.Grant, refresh); err != nil {
		w.options.Logger.Warn("seal mailbox grant with the current key", "provider", item.Mailbox.Grant.Provider, "workspace", item.WorkspaceID, "error", err)
	}
}

// storeGrant seals the refresh token with the first key and replaces the grant held (by a delivery or the
// keep-alive). The holder then holds the new seal, so a failure recorded later matches the stored row.
func (w *Worker) storeGrant(ctx context.Context, sender mailboxProvider, workspaceID string, held *store.SealedGrant, refresh []byte) error {
	keyID, sealed, err := sender.keys().Seal(refresh, sender.grantContext(workspaceID))
	if err != nil {
		return err
	}
	replacement := store.SealedGrant{KeyID: keyID, Sealed: sealed, Address: held.Address, Provider: held.Provider}
	replaced, err := w.store.ReplaceMailboxGrant(ctx, workspaceID, *held, replacement, w.options.Now())
	if err != nil {
		return err
	}
	// Not replaced: a reconnect changed the row meanwhile, and the holder keeps matching nothing.
	if replaced {
		*held = replacement
	}
	return nil
}

// breakConnection marks the channel's mailbox connection failed and fails the reply with the same reason.
func (w *Worker) breakConnection(ctx context.Context, item store.Delivery, provider, code string) channels.SendResult {
	w.options.Logger.Warn("mailbox connection failed", "provider", provider, "workspace", item.WorkspaceID, "reason", code)
	if err := w.store.MarkMailboxFailed(ctx, item.WorkspaceID, item.Mailbox.Grant, code, w.options.Now()); err != nil {
		w.options.Logger.Error("mark mailbox connection failed", "provider", provider, "workspace", item.WorkspaceID, "error", err)
	}
	return channels.Failed(domain.MailboxFailureText(provider, code))
}

// retry schedules another attempt; the outbox fails the reply once every attempt is spent.
func (w *Worker) retry(_ context.Context, _ store.Delivery, err error) channels.SendResult {
	return channels.Retry(truncate(err.Error()))
}
