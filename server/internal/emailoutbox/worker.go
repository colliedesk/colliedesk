// Package emailoutbox sends queued replies through SMTP with retries and backoff.
package emailoutbox

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

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

// ProcessDue sends every due delivery once and returns how many were attempted.
func (w *Worker) ProcessDue(ctx context.Context) (int, error) {
	now := w.options.Now()
	items, err := w.store.ClaimDueDeliveries(ctx, now, w.options.BatchSize, w.options.Lease)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, item := range items {
		if err := w.deliver(ctx, item, now); err != nil {
			errs = append(errs, err)
		}
	}
	return len(items), errors.Join(errs...)
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

func (w *Worker) deliver(ctx context.Context, item store.Delivery, now time.Time) error {
	if item.SupportAddress == "" {
		return w.store.MarkDeliveryFailed(ctx, item, "The email channel is not connected.")
	}
	message, err := email.Compose(email.Outgoing{
		FromName: item.WorkspaceName, FromAddress: item.SupportAddress, ReplyTo: email.PlusAddress(item.SupportAddress, item.ReplyToken),
		ToName: item.ContactName, ToAddress: item.ContactEmail, Subject: item.Subject, Body: item.Body,
		MessageID: item.EmailMessageID, InReplyTo: item.InReplyTo, References: item.References, Date: now,
	})
	if err != nil {
		return w.store.MarkDeliveryFailed(ctx, item, truncate(err.Error()))
	}
	sendErr := w.sender.Send(ctx, item.SupportAddress, []string{item.ContactEmail}, message)
	switch {
	case sendErr == nil:
		return w.store.MarkDeliverySent(ctx, item, now)
	case email.IsPermanent(sendErr) || item.Attempts >= w.options.MaxAttempts:
		w.options.Logger.Warn("email delivery failed", "workspace", item.WorkspaceID, "message", item.MessageID, "attempts", item.Attempts, "error", sendErr)
		return w.store.MarkDeliveryFailed(ctx, item, truncate(sendErr.Error()))
	default:
		return w.store.MarkDeliveryRetry(ctx, item, now.Add(Backoff(item.Attempts)), truncate(sendErr.Error()))
	}
}

func truncate(value string) string {
	if len(value) <= maxErrorLength {
		return value
	}
	return strings.ToValidUTF8(value[:maxErrorLength], "") + "…"
}
