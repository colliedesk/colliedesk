// Package emailoutbox is the email channel's sender: it sends queued replies through SMTP, or through
// the workspace's connected Gmail account. The shared outbox worker (channels/outbox) claims, retries
// and backs off; Worker runs it with this sender, for the server and the tests of email delivery.
package emailoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	defaultMaxAttempts = 8
	defaultLease       = 5 * time.Minute
	defaultBatchSize   = 20
	maxErrorLength     = 500
	defaultHourlyLimit = 300
	limitDeferral      = 10 * time.Minute
	// pauseDeferral is how often a paused workspace's replies are looked at again; resuming makes them due at once.
	pauseDeferral = 10 * time.Minute
)

// Sender hands one composed reply to a mail relay: to must accept it, a refused copy is skipped and
// returned.
type Sender interface {
	SendCopying(ctx context.Context, from, to string, copies []string, message []byte) ([]email.RefusedCopy, error)
}

// Options tune the worker; zero values use the defaults.
type Options struct {
	Now         func() time.Time
	MaxAttempts int
	Lease       time.Duration
	BatchSize   int
	Logger      *slog.Logger
	// FromPolicy and MailFromDomain choose the sending identity (see email.FromPolicyVerified).
	FromPolicy     string
	MailFromDomain string
	// Storage holds the files attached to replies; without it a reply with files fails permanently.
	Storage attachments.Storage
	// HourlyLimit caps replies sent per workspace per hour; zero uses the default.
	HourlyLimit int
	// Google sends the replies of workspaces that connected Gmail; nil fails them with the reason.
	Google *google.Config
}

// Worker drains the email outbox.
type Worker struct {
	store   *store.Store
	sender  Sender
	options Options
}

// New builds a worker over the store and sender. A nil sender (no SMTP relay) still sends through Gmail.
func New(st *store.Store, sender Sender, options Options) *Worker {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = defaultMaxAttempts
	}
	if options.Lease <= 0 {
		options.Lease = defaultLease
	}
	if options.BatchSize <= 0 {
		options.BatchSize = defaultBatchSize
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.HourlyLimit <= 0 {
		options.HourlyLimit = defaultHourlyLimit
	}
	if options.FromPolicy == "" {
		options.FromPolicy = email.FromPolicyVerified
	}
	return &Worker{store: st, sender: sender, options: options}
}

// Backoff is the wait after the given failed attempt: 30s, 1m, 2m, ... capped at one hour.
func Backoff(attempt int) time.Duration { return outbox.Backoff(attempt) }

// worker is the shared outbox worker that sends email through w.
func (w *Worker) worker() *outbox.Worker {
	return outbox.New(w.store, outbox.Options{Now: w.options.Now, MaxAttempts: w.options.MaxAttempts, Lease: w.options.Lease,
		BatchSize: w.options.BatchSize, Logger: w.options.Logger, Senders: map[channels.Type]channels.OutboundPart{domain.ChannelEmail: w}})
}

// ProcessDue sends every due delivery once and returns how many were attempted.
func (w *Worker) ProcessDue(ctx context.Context) (int, error) { return w.worker().ProcessDue(ctx) }

// Run processes the outbox every interval until the context ends.
func (w *Worker) Run(ctx context.Context, interval time.Duration) { w.worker().Run(ctx, interval) }

// emailHost is what the email channel needs of the store that queues a reply.
type emailHost interface {
	PrepareEmailReply(ctx context.Context, tx pgx.Tx, request channels.PrepareRequest) (channels.Prepared, error)
}

// Prepare fixes the reply's Message-ID and reply token and applies the trial's rules, in the
// transaction that stores it.
func (w *Worker) Prepare(ctx context.Context, tx pgx.Tx, request channels.PrepareRequest) (channels.Prepared, error) {
	host, ok := request.Host.(emailHost)
	if !ok {
		return channels.Prepared{}, errors.New("the email channel needs the store to queue a reply")
	}
	return host.PrepareEmailReply(ctx, tx, request)
}

// Send sends one claimed reply through SMTP or Gmail.
func (w *Worker) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	sender := *w
	if host, ok := kit.Host().(*store.Store); ok {
		sender.store = host
	}
	item, err := sender.store.EmailDelivery(ctx, out.WorkspaceID, out.DeliveryID, out.Attempt)
	if err != nil {
		sender.options.Logger.Warn("load email delivery", "workspace", out.WorkspaceID, "error", err)
		return channels.Failed("The reply could not be loaded for sending.")
	}
	return sender.deliver(ctx, item)
}

func (w *Worker) deliver(ctx context.Context, item store.Delivery) channels.SendResult {
	if item.SupportAddress == "" {
		return channels.Failed("The email channel is not connected.")
	}
	if item.EmailPaused {
		result := channels.Defer(w.options.Now().Add(pauseDeferral), domain.EmailPausedDeliveryNote)
		result.HeldByPause = true
		return result
	}
	sent, err := w.store.CountSentSince(ctx, item.WorkspaceID, w.options.Now().Add(-time.Hour))
	if err != nil {
		return channels.Retry(truncate(err.Error()))
	}
	if sent >= w.options.HourlyLimit {
		return channels.Defer(w.options.Now().Add(limitDeferral),
			fmt.Sprintf("Waiting: this workspace reached its hourly limit of %d emailed replies.", w.options.HourlyLimit))
	}
	if item.Transport == transportGmail {
		return w.deliverGmail(ctx, item)
	}
	if w.sender == nil {
		return channels.Failed("Outgoing email is not configured on this server. Ask the administrator to set SMTP_HOST.")
	}
	from, envelope, ok := w.identity(item)
	if !ok {
		return channels.Failed("Verify the support address domain in Settings before replies can be emailed.")
	}
	files, err := w.loadFiles(ctx, item)
	if errors.Is(err, errFileMissing) {
		return channels.Failed(truncate(err.Error()))
	}
	if err != nil {
		return channels.Retry(truncate(err.Error()))
	}
	copies, unusable := usableCopies(item.CC)
	message, err := w.compose(item, from, copies, files)
	if err != nil {
		return channels.Failed(truncate(err.Error()))
	}
	refused, sendErr := w.sender.SendCopying(ctx, envelope, item.ContactEmail, copies, message)
	switch {
	case sendErr == nil:
		return channels.Sent(truncate(missedCopies(append(unusable, refused...))))
	case email.IsPermanent(sendErr):
		return channels.Failed(truncate(sendErr.Error()))
	default:
		return channels.Retry(truncate(sendErr.Error()))
	}
}

// compose renders the reply from the given identity. Reply-To stays the support address with the
// conversation token on every transport, so the customer's answer threads even when a relay or Gmail
// rewrites our Message-ID.
func (w *Worker) compose(item store.Delivery, from mail.Address, copies []string, files []email.File) ([]byte, error) {
	return email.Compose(email.Outgoing{Files: files,
		FromName: from.Name, FromAddress: from.Address, ReplyTo: email.PlusAddress(item.SupportAddress, item.ReplyToken),
		ReplyToName: item.SenderName,
		ToName:      item.ContactName, ToAddress: item.ContactEmail, Cc: copies, Subject: item.Subject, Body: item.Body,
		MessageID: item.EmailMessageID, InReplyTo: item.InReplyTo, References: item.References, Date: w.options.Now(), Opening: item.Opening,
	})
}

// usableCopies splits a stored Cc list into the addresses a message can carry and the rest, which are
// reported instead of failing the reply to the customer.
func usableCopies(cc []string) ([]string, []email.RefusedCopy) {
	var usable []string
	var unusable []email.RefusedCopy
	for _, address := range cc {
		if email.ValidAddress(address) {
			usable = append(usable, address)
		} else {
			unusable = append(unusable, email.RefusedCopy{Address: address, Reason: "not a plain email address"})
		}
	}
	return usable, unusable
}

// missedCopies describes the copied addresses a sent reply did not reach; empty when it reached all.
func missedCopies(missed []email.RefusedCopy) string {
	if len(missed) == 0 {
		return ""
	}
	parts := make([]string, len(missed))
	for i, copy := range missed {
		parts[i] = copy.Address + " (" + copy.Reason + ")"
	}
	return "Sent to the customer, but not to: " + strings.Join(parts, "; ")
}

// errFileMissing marks a file that is gone or no longer matches its checksum: permanent for the delivery.
var errFileMissing = errors.New("attached file is unusable")

// loadFiles reads the reply's files from storage. A file that is gone is permanent: sending the reply
// without it would reach the customer as a text-only message the agent did not write.
func (w *Worker) loadFiles(ctx context.Context, item store.Delivery) ([]email.File, error) {
	files := make([]email.File, 0, len(item.Files))
	for _, stored := range item.Files {
		if w.options.Storage == nil {
			return nil, fmt.Errorf("%w: attachment storage is not configured, so %q cannot be sent", errFileMissing, stored.Filename)
		}
		data, err := readStored(ctx, w.options.Storage, stored.StorageKey)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("%w: %q is no longer in attachment storage, so the reply was not sent", errFileMissing, stored.Filename)
		case err != nil:
			return nil, fmt.Errorf("read attachment %q: %w", stored.Filename, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != stored.SHA256 {
			return nil, fmt.Errorf("%w: %q in attachment storage does not match what was uploaded, so the reply was not sent", errFileMissing, stored.Filename)
		}
		files = append(files, email.File{Filename: stored.Filename, ContentType: stored.ContentType, Data: data})
	}
	return files, nil
}

func readStored(ctx context.Context, storage attachments.Storage, key string) ([]byte, error) {
	body, err := storage.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, attachments.MaxOutgoingBytes+1))
}

// identity returns the From header and envelope sender. A shared relay may send as the support address
// only for a verified domain; otherwise it sends from the platform domain and keeps the support address
// in Reply-To, so the customer's reply still reaches the workspace and threads.
func (w *Worker) identity(item store.Delivery) (mail.Address, string, bool) {
	if w.options.FromPolicy == email.FromPolicyAny || item.DomainVerified {
		return mail.Address{Name: item.SenderName, Address: item.SupportAddress}, item.SupportAddress, true
	}
	if w.options.MailFromDomain == "" || item.ReplyToken == "" {
		return mail.Address{}, "", false
	}
	address := email.PlatformReplyFrom(item.ReplyToken, item.EmailMessageID, w.options.MailFromDomain)
	return mail.Address{Name: item.SenderName + " via Colliedesk", Address: address}, address, true
}

func truncate(value string) string {
	if len(value) <= maxErrorLength {
		return value
	}
	return strings.ToValidUTF8(value[:maxErrorLength], "") + "…"
}
