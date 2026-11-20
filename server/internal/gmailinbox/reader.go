// Package gmailinbox brings new mail of connected Gmail inboxes into their email channels, on a server whose
// operator turned reading on (google.read_mailbox) and for grants that include gmail.readonly. Each mailbox is
// read from where the last read stopped (Gmail's history id), and each new inbox message goes through the
// same inbound pipeline as forwarded mail, so a message that also arrives by forwarding is a duplicate, not a
// second copy. Reading starts at the connection: mail that was in the inbox before is not brought in.
// docs/internals/email.md "Reading the Gmail inbox".
package gmailinbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// batch is how many mailboxes one round reads, least recently read first.
	batch = 50
	// listMargin reads a little before the last read when the history id is gone: Gmail's after: counts
	// whole seconds and its clock is not ours. What is already stored is a duplicate.
	listMargin = time.Hour
	// maxRawBytes and maxFiles are what the inbound route accepts of forwarded mail.
	maxRawBytes = 25 << 20
	maxFiles    = 100
)

// Pipeline is the inbound pipeline the messages go through (pipeline.Pipeline).
type Pipeline interface {
	Receive(ctx context.Context, channel channels.Channel, raws []channels.Raw) ([]channels.IngestResult, error)
}

// Reader reads the connected Gmail inboxes.
type Reader struct {
	Store    *store.Store
	Google   google.Config
	Pipeline Pipeline
	Logger   *slog.Logger
	Now      func() time.Time
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Reader) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Enabled reports whether this server reads Gmail inboxes at all.
func (r *Reader) Enabled() bool { return r.Google.Enabled() && r.Google.ReadMailbox }

// Run reads once at once and then every interval until the context ends.
func (r *Reader) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := r.ReadDue(ctx); err != nil && ctx.Err() == nil {
			r.logger().Warn("read gmail inboxes", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReadDue reads every mailbox due in one round and returns how many messages it brought in (created or
// threaded). One mailbox failing does not stop the others.
func (r *Reader) ReadDue(ctx context.Context) (int, error) {
	if !r.Enabled() {
		return 0, nil
	}
	due, err := r.Store.MailboxesToRead(ctx, batch)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, ref := range due {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		count, err := r.Read(ctx, ref.WorkspaceID, ref.ChannelID)
		total += count
		if err != nil {
			r.logger().Warn("read a gmail inbox", "workspace", ref.WorkspaceID, "channel", ref.ChannelID, "error", err)
		}
	}
	return total, nil
}

// Read brings the new mail of one channel's Gmail inbox in. It saves where it stopped only once every
// message before it is stored, so an error reads the same messages again next time (duplicates are known).
func (r *Reader) Read(ctx context.Context, workspaceID, channelID string) (int, error) {
	inbox, err := r.Store.ReadableMailbox(ctx, workspaceID, channelID)
	if err != nil || inbox == nil {
		return 0, err
	}
	access, err := r.accessToken(ctx, workspaceID, inbox)
	if err != nil {
		return 0, err
	}
	var ids []string
	var next string
	if inbox.HistoryID == "" {
		// The first read lists what arrived since the connection, so mail between the consent and this
		// read is not skipped; Gmail's after: counts whole seconds, so it starts at the connection's second.
		ids, next, err = r.listSince(ctx, access, inbox.ConnectedAt.Truncate(time.Second).Add(-time.Second))
	} else {
		ids, next, err = r.newMessages(ctx, access, inbox)
	}
	if err != nil {
		return 0, r.refused(ctx, workspaceID, channelID, inbox, err)
	}
	channel, err := r.Store.GetChannel(ctx, workspaceID, channelID)
	if err != nil {
		return 0, fmt.Errorf("load email channel: %w", err)
	}
	brought := 0
	for _, id := range ids {
		created, err := r.bringIn(ctx, access, channel, inbox, id)
		if err != nil {
			return brought, r.refused(ctx, workspaceID, channelID, inbox, err)
		}
		if created {
			brought++
		}
	}
	if next == "" {
		return brought, nil
	}
	_, err = r.Store.SaveInboxRead(ctx, workspaceID, channelID, inbox.ConnectedAt, next, r.now())
	return brought, err
}

// newMessages lists the inbox messages added since the last read, oldest first, and the history id to
// start from next time. It lists them all: a read that stopped part way would start over at the same id.
func (r *Reader) newMessages(ctx context.Context, access string, inbox *store.MailboxInbox) ([]string, string, error) {
	var ids []string
	var page google.HistoryPage
	var err error
	for {
		page, err = r.Google.InboxHistory(ctx, access, inbox.HistoryID, page.NextPageToken)
		if errors.Is(err, google.ErrHistoryGone) {
			since := inbox.ConnectedAt
			if inbox.ReadAt != nil {
				since = inbox.ReadAt.Add(-listMargin)
			}
			return r.listSince(ctx, access, since)
		}
		if err != nil {
			return nil, "", err
		}
		ids = appendNew(ids, page.MessageIDs...)
		if page.NextPageToken == "" {
			return ids, page.HistoryID, nil
		}
	}
}

// listSince lists the inbox since a time, oldest first: for the first read (since the connection) and as the
// way back when Gmail no longer keeps the history id (since the last read). The profile's history id comes
// first, so nothing that arrives while listing is missed.
func (r *Reader) listSince(ctx context.Context, access string, since time.Time) ([]string, string, error) {
	profile, err := r.Google.Profile(ctx, access)
	if err != nil {
		return nil, "", err
	}
	var newestFirst []string
	page := google.InboxPage{}
	for {
		page, err = r.Google.ListInbox(ctx, access, since, page.NextPageToken)
		if err != nil {
			return nil, "", err
		}
		newestFirst = appendNew(newestFirst, page.MessageIDs...)
		if page.NextPageToken == "" {
			break
		}
	}
	slices.Reverse(newestFirst)
	return newestFirst, profile.HistoryID, nil
}

func appendNew(ids []string, more ...string) []string {
	for _, id := range more {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// bringIn reads one message and gives it to the pipeline. It reports whether a conversation got a message
// (created or threaded). A message that is gone, too large, or not the channel's is skipped with a log line.
func (r *Reader) bringIn(ctx context.Context, access string, channel channels.Channel, inbox *store.MailboxInbox, id string) (bool, error) {
	message, err := r.Google.Message(ctx, access, id)
	if errors.Is(err, google.ErrMessageGone) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log := r.logger().With("workspace", channel.WorkspaceID, "channel", channel.ID, "gmail_message", id)
	if slices.Contains(message.LabelIDs, "SPAM") || slices.Contains(message.LabelIDs, "DRAFT") {
		return false, nil
	}
	if len(message.Raw) > maxRawBytes {
		log.Warn("a gmail message is larger than 25 MiB and was not brought in", "bytes", len(message.Raw))
		return false, nil
	}
	parsed, err := email.Parse(message.Raw, email.GmailAuthServID)
	if err != nil {
		log.Warn("a gmail message has no usable sender and was not brought in", "error", err)
		return false, nil
	}
	if len(parsed.Files) > maxFiles {
		log.Warn("a gmail message has more than 100 attachments and was not brought in", "files", len(parsed.Files))
		return false, nil
	}
	if !forChannel(parsed, inbox) {
		return false, nil
	}
	// It passed the support mailbox, so, like forwarded mail, it shows that the mailbox reaches Colliedesk
	// (not Direct); it passed no forwarder (not Platform).
	parsed.Gmail = true
	results, err := r.Pipeline.Receive(ctx, channel, []channels.Raw{{Parsed: parsed, Body: message.Raw}})
	switch {
	case errors.Is(err, domain.ErrMailboxClosed):
		log.Info("the email channel does not accept new mail; a gmail message was not brought in")
		return false, nil
	case errors.Is(err, store.ErrRateLimited):
		// The loop guard: one sender wrote more than the window allows, as a forwarder would be refused.
		log.Warn("a gmail message was not brought in: its sender is over the inbound rate limit", "from", parsed.FromAddress)
		return false, nil
	case err != nil:
		return false, err
	}
	for _, result := range results {
		if result.Status == channels.ResultCreated || result.Status == channels.ResultThreaded {
			return true, nil
		}
	}
	return false, nil
}

// forChannel reports whether the message is the channel's: every message of a mailbox that is the support
// address, else only mail sent to the support address (an alias of a larger account).
func forChannel(parsed email.Inbound, inbox *store.MailboxInbox) bool {
	if email.SameMailbox(inbox.Grant.Address, inbox.SupportAddress) {
		return true
	}
	for _, recipient := range parsed.Recipients {
		if email.SameMailbox(recipient, inbox.SupportAddress) {
			return true
		}
	}
	return false
}

// accessToken opens the grant and gets an access token, storing a rotated refresh token. A grant Google
// no longer honours marks the connection failed, as a send would.
func (r *Reader) accessToken(ctx context.Context, workspaceID string, inbox *store.MailboxInbox) (string, error) {
	keys := r.Google.Keys
	refresh, err := keys.Open(inbox.Grant.KeyID, inbox.Grant.Sealed, google.GrantContext(workspaceID))
	if err != nil {
		return "", fmt.Errorf("the gmail grant does not open with this server's keys (key %s)", inbox.Grant.KeyID)
	}
	access, rotated, err := r.Google.AccessToken(ctx, string(refresh))
	if errors.Is(err, google.ErrRevoked) {
		if markErr := r.Store.MarkMailboxFailed(ctx, workspaceID, inbox.Grant, domain.MailboxRevoked, r.now()); markErr != nil {
			return "", markErr
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	if rotated != "" {
		keyID, sealed, err := keys.Seal([]byte(rotated), google.GrantContext(workspaceID))
		if err != nil {
			return "", err
		}
		if _, err := r.Store.ReplaceMailboxGrant(ctx, workspaceID, inbox.Grant, store.SealedGrant{KeyID: keyID, Sealed: sealed}, r.now()); err != nil {
			return "", err
		}
	}
	return access, nil
}

// refused records what a Gmail answer says about reading: a refusal that is not a rate limit (gmail.readonly
// withdrawn) stops reading the inbox, while sending goes on. Anything else is tried again next round.
func (r *Reader) refused(ctx context.Context, workspaceID, channelID string, inbox *store.MailboxInbox, err error) error {
	var apiErr *google.APIError
	if errors.As(err, &apiErr) && apiErr.ConnectionBroken() {
		if markErr := r.Store.RefuseInboxRead(ctx, workspaceID, channelID, inbox.ConnectedAt, domain.MailboxPermission); markErr != nil {
			return markErr
		}
	}
	return err
}
