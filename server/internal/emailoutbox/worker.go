// Package emailoutbox sends queued replies through SMTP with retries and backoff.
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
	"github.com/colliedesk/colliedesk/server/internal/email"
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
)

// Sender hands one composed message to a mail relay.
type Sender interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
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
}

// Worker drains the email outbox.
type Worker struct {
	store   *store.Store
	sender  Sender
	options Options
}

// New builds a worker over the store and sender.
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
	sent, err := w.store.CountSentSince(ctx, item.WorkspaceID, w.options.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if sent >= w.options.HourlyLimit {
		return w.store.DeferDelivery(ctx, item, w.options.Now().Add(limitDeferral),
			fmt.Sprintf("Waiting: this workspace reached its hourly limit of %d emailed replies.", w.options.HourlyLimit))
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
	message, err := email.Compose(email.Outgoing{Files: files,
		FromName: from.Name, FromAddress: from.Address, ReplyTo: email.PlusAddress(item.SupportAddress, item.ReplyToken),
		ReplyToName: item.WorkspaceName,
		ToName:      item.ContactName, ToAddress: item.ContactEmail, Subject: item.Subject, Body: item.Body,
		MessageID: item.EmailMessageID, InReplyTo: item.InReplyTo, References: item.References, Date: w.options.Now(),
	})
	if err != nil {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	sendErr := w.sender.Send(ctx, envelope, []string{item.ContactEmail}, message)
	switch {
	case sendErr == nil:
		return w.store.MarkDeliverySent(ctx, item, w.options.Now())
	case email.IsPermanent(sendErr) || item.Attempts >= w.options.MaxAttempts:
		w.options.Logger.Warn("email delivery failed", "workspace", item.WorkspaceID, "message", item.MessageID, "attempts", item.Attempts, "error", sendErr)
		return w.store.MarkDeliveryFailed(ctx, item, truncate(sendErr.Error()))
	default:
		return w.store.MarkDeliveryRetry(ctx, item, w.options.Now().Add(Backoff(item.Attempts)), truncate(sendErr.Error()))
	}
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
