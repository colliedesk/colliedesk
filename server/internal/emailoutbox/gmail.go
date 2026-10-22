package emailoutbox

import (
	"context"
	"errors"
	"fmt"
	"net/mail"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const transportGmail = "gmail"

// deliverGmail sends a reply accepted for Gmail from the connected account. It never hands the reply
// to SMTP: a removed or broken connection fails the reply with the reason, and only a retry (which
// chooses the transport again) sends it another way.
func (w *Worker) deliverGmail(ctx context.Context, item store.Delivery) error {
	mailbox := item.Mailbox
	switch {
	case mailbox == nil:
		return w.store.MarkDeliveryFailed(ctx, item, domain.MailboxDisconnectedNote)
	case mailbox.Status == domain.MailboxFailed:
		return w.store.MarkDeliveryFailed(ctx, item, domain.MailboxFailureText(mailbox.ErrorCode))
	case w.options.Google == nil || !w.options.Google.Enabled():
		return w.store.MarkDeliveryFailed(ctx, item, domain.MailboxNotConfiguredNote)
	}
	config := *w.options.Google
	refresh, err := config.Keys.Open(mailbox.Grant.KeyID, mailbox.Grant.Sealed, google.GrantContext(item.WorkspaceID))
	if err != nil {
		// This server's keys, not the grant: a replica with the right MAILBOX_TOKEN_KEYS sends it, so the
		// connection stays as it is and the reply waits like any temporary failure.
		w.options.Logger.Warn("gmail grant does not open with this server's keys", "workspace", item.WorkspaceID, "key", mailbox.Grant.KeyID)
		return w.retry(ctx, item, errors.New(domain.MailboxKeyNote))
	}
	if mailbox.Grant.KeyID != config.Keys.Current() {
		w.resealGrant(ctx, config, item, refresh)
	}
	files, err := w.loadFiles(ctx, item)
	if errors.Is(err, errFileMissing) {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	if err != nil {
		return w.retry(ctx, item, err)
	}
	copies, unusable := usableCopies(item.CC)
	message, err := w.compose(item, mail.Address{Name: item.SenderName, Address: mailbox.Address}, copies, files)
	if err != nil {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	err = w.sendGmail(ctx, config, item, string(refresh), message)
	var apiErr *google.APIError
	switch {
	case err == nil:
		return w.store.MarkDeliverySent(ctx, item, w.options.Now(), truncate(missedCopies(unusable)))
	case errors.Is(err, google.ErrRevoked):
		return w.breakConnection(ctx, item, domain.MailboxRevoked)
	case errors.As(err, &apiErr) && apiErr.ConnectionBroken():
		return w.breakConnection(ctx, item, domain.MailboxPermission)
	case errors.As(err, &apiErr) && !apiErr.Temporary():
		w.options.Logger.Warn("gmail delivery failed", "workspace", item.WorkspaceID, "message", item.MessageID, "status", apiErr.Status, "reason", apiErr.Reason)
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	default:
		return w.retry(ctx, item, err)
	}
}

// sendGmail gets an access token and sends; a refused access token is refreshed and tried once more.
func (w *Worker) sendGmail(ctx context.Context, config google.Config, item store.Delivery, refresh string, message []byte) error {
	for range 2 {
		access, rotated, err := config.AccessToken(ctx, refresh)
		if err != nil {
			return err
		}
		if rotated != "" {
			w.keepRotatedGrant(ctx, config, item, rotated)
			refresh = rotated
		}
		err = config.Send(ctx, access, message)
		if !errors.Is(err, google.ErrUnauthorized) {
			return err
		}
	}
	return fmt.Errorf("%w twice in a row", google.ErrUnauthorized)
}

// keepRotatedGrant stores a refresh token Google replaced. Failing to store it only logs: the old one
// keeps working until Google says otherwise, which then shows as a broken connection.
func (w *Worker) keepRotatedGrant(ctx context.Context, config google.Config, item store.Delivery, rotated string) {
	if err := w.storeGrant(ctx, config, item, []byte(rotated)); err != nil {
		w.options.Logger.Warn("keep rotated Gmail grant", "workspace", item.WorkspaceID, "error", err)
	}
}

// resealGrant seals a grant opened with an older key again with the first key, so the older key can be
// removed from MAILBOX_TOKEN_KEYS. A failure only logs; the reply goes on with the grant it opened.
func (w *Worker) resealGrant(ctx context.Context, config google.Config, item store.Delivery, refresh []byte) {
	if err := w.storeGrant(ctx, config, item, refresh); err != nil {
		w.options.Logger.Warn("seal Gmail grant with the current key", "workspace", item.WorkspaceID, "error", err)
	}
}

// storeGrant seals the refresh token with the first key and replaces the grant this delivery holds. The
// delivery then holds the new seal, so a failure recorded later in the same delivery matches the stored row.
func (w *Worker) storeGrant(ctx context.Context, config google.Config, item store.Delivery, refresh []byte) error {
	keyID, sealed, err := config.Keys.Seal(refresh, google.GrantContext(item.WorkspaceID))
	if err != nil {
		return err
	}
	replacement := store.SealedGrant{KeyID: keyID, Sealed: sealed, Address: item.Mailbox.Grant.Address}
	replaced, err := w.store.ReplaceMailboxGrant(ctx, item.WorkspaceID, item.Mailbox.Grant, replacement, w.options.Now())
	if err != nil {
		return err
	}
	// Not replaced: a reconnect changed the row meanwhile, and this delivery keeps matching nothing.
	if replaced {
		item.Mailbox.Grant = replacement
	}
	return nil
}

// breakConnection marks the workspace's Gmail connection failed and fails the reply with the same reason.
func (w *Worker) breakConnection(ctx context.Context, item store.Delivery, code string) error {
	w.options.Logger.Warn("gmail connection failed", "workspace", item.WorkspaceID, "reason", code)
	if err := w.store.MarkMailboxFailed(ctx, item.WorkspaceID, item.Mailbox.Grant, code, w.options.Now()); err != nil {
		return errors.Join(err, w.store.MarkDeliveryFailed(ctx, item, domain.MailboxFailureText(code)))
	}
	return w.store.MarkDeliveryFailed(ctx, item, domain.MailboxFailureText(code))
}

// retry schedules another attempt, or fails the reply once every attempt is spent.
func (w *Worker) retry(ctx context.Context, item store.Delivery, err error) error {
	if item.Attempts >= w.options.MaxAttempts {
		w.options.Logger.Warn("email delivery failed", "workspace", item.WorkspaceID, "message", item.MessageID, "attempts", item.Attempts, "error", err)
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	return w.store.MarkDeliveryRetry(ctx, item, w.options.Now().Add(Backoff(item.Attempts)), truncate(err.Error()))
}
