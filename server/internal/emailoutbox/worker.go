// Package emailoutbox sends queued replies through SMTP, or through the workspace's connected Gmail
// account, with retries and backoff.
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
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	defaultMaxAttempts = 8
	defaultLease       = 5 * time.Minute
	defaultBatchSize   = 20
	firstBackoff       = 30 * time.Second
	maxBackoff         = time.Hour
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
func Backoff(attempt int) time.Duration {
	delay := firstBackoff
	for range max(attempt-1, 0) {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}

// ProcessDue sends every due delivery once and returns how many were attempted. It claims one row at
// a time so a lease never has to cover more than one send.
func (w *Worker) ProcessDue(ctx context.Context) (int, error) {
	var errs []error
	attempted := 0
	for attempted < w.options.BatchSize {
		items, err := w.store.ClaimDueDeliveries(ctx, w.options.Now(), 1, w.options.Lease)
		if err != nil {
			errs = append(errs, err)
		}
		if len(items) == 0 {
			if err == nil {
				break
			}
			attempted++
			continue
		}
		attempted++
		if err := w.deliver(ctx, items[0]); err != nil {
			errs = append(errs, err)
		}
	}
	return attempted, errors.Join(errs...)
}

// Run processes the outbox every interval until the context ends.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.ProcessDue(ctx); err != nil && ctx.Err() == nil {
			w.options.Logger.Error("email outbox", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) deliver(ctx context.Context, item store.Delivery) error {
	if item.Attempts > w.options.MaxAttempts {
		return w.store.MarkDeliveryFailed(ctx, item, fmt.Sprintf("Gave up after %d attempts.", w.options.MaxAttempts))
	}
	if item.SupportAddress == "" {
		return w.store.MarkDeliveryFailed(ctx, item, "The email channel is not connected.")
	}
	if item.EmailPaused {
		return w.store.DeferDeliveryForPause(ctx, item, w.options.Now().Add(pauseDeferral), domain.EmailPausedDeliveryNote)
	}
	sent, err := w.store.CountSentSince(ctx, item.WorkspaceID, w.options.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if sent >= w.options.HourlyLimit {
		return w.store.DeferDelivery(ctx, item, w.options.Now().Add(limitDeferral),
			fmt.Sprintf("Waiting: this workspace reached its hourly limit of %d emailed replies.", w.options.HourlyLimit))
	}
	if item.Transport == transportGmail {
		return w.deliverGmail(ctx, item)
	}
	if w.sender == nil {
		return w.store.MarkDeliveryFailed(ctx, item, "Outgoing email is not configured on this server. Ask the administrator to set SMTP_HOST.")
	}
	from, envelope, ok := w.identity(item)
	if !ok {
		return w.store.MarkDeliveryFailed(ctx, item, "Verify the support address domain in Settings before replies can be emailed.")
	}
	files, err := w.loadFiles(ctx, item)
	if errors.Is(err, errFileMissing) {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	if err != nil {
		return w.store.MarkDeliveryRetry(ctx, item, w.options.Now().Add(Backoff(item.Attempts)), truncate(err.Error()))
	}
	copies, unusable := usableCopies(item.CC)
	message, err := w.compose(item, from, copies, files)
	if err != nil {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	refused, sendErr := w.sender.SendCopying(ctx, envelope, item.ContactEmail, copies, message)
	switch {
	case sendErr == nil:
		return w.store.MarkDeliverySent(ctx, item, w.options.Now(), truncate(missedCopies(append(unusable, refused...))))
	case email.IsPermanent(sendErr) || item.Attempts >= w.options.MaxAttempts:
		w.options.Logger.Warn("email delivery failed", "workspace", item.WorkspaceID, "message", item.MessageID, "attempts", item.Attempts, "error", sendErr)
		return w.store.MarkDeliveryFailed(ctx, item, truncate(sendErr.Error()))
	default:
		return w.store.MarkDeliveryRetry(ctx, item, w.options.Now().Add(Backoff(item.Attempts)), truncate(sendErr.Error()))
	}
}

// compose renders the reply from the given identity. Reply-To stays the support address with the
// conversation token on every transport, so the customer's answer threads even when a relay or Gmail
// rewrites our Message-ID.
func (w *Worker) compose(item store.Delivery, from mail.Address, copies []string, files []email.File) ([]byte, error) {
	return email.Compose(email.Outgoing{Files: files,
		FromName: from.Name, FromAddress: from.Address, ReplyTo: email.PlusAddress(item.SupportAddress, item.ReplyToken),
		ReplyToName: item.WorkspaceName,
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
		return mail.Address{Name: item.WorkspaceName, Address: item.SupportAddress}, item.SupportAddress, true
	}
	if w.options.MailFromDomain == "" || item.ReplyToken == "" {
		return mail.Address{}, "", false
	}
	address := email.PlatformReplyFrom(item.ReplyToken, item.EmailMessageID, w.options.MailFromDomain)
	return mail.Address{Name: item.WorkspaceName + " via Colliedesk", Address: address}, address, true
}

func truncate(value string) string {
	if len(value) <= maxErrorLength {
		return value
	}
	return strings.ToValidUTF8(value[:maxErrorLength], "") + "…"
}
