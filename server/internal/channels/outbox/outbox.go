// Package outbox sends queued replies of every channel through their type's sender, with leases,
// retries and backoff. It knows no channel type.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	defaultMaxAttempts = 8
	defaultLease       = 5 * time.Minute
	defaultBatchSize   = 20
	firstBackoff       = 30 * time.Second
	maxBackoff         = time.Hour
)

// Options tune the worker; zero values use the defaults.
type Options struct {
	Now         func() time.Time
	MaxAttempts int
	Lease       time.Duration
	BatchSize   int
	Logger      *slog.Logger
	// Senders replace the registry's sender of a type (a test's relay, a facade's own options).
	Senders map[channels.Type]channels.OutboundPart
	// Skip leaves the replies of these types to another worker (a server that cannot send email
	// leaves email to the servers that can).
	Skip []channels.Type
}

// Worker drains the outbox.
type Worker struct {
	store   *store.Store
	options Options
}

// New builds a worker over a store that has the channel registry.
func New(st *store.Store, options Options) *Worker {
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
	return &Worker{store: st, options: options}
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
		items, err := w.store.ClaimDueOutbox(ctx, w.options.Now(), 1, w.options.Lease, w.options.Skip...)
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
			w.options.Logger.Error("outbox", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) sender(kind channels.Type) channels.OutboundPart {
	if sender, ok := w.options.Senders[kind]; ok {
		return sender
	}
	if adapter, ok := w.store.Channels().Adapter(kind); ok {
		return adapter.Outbound()
	}
	return nil
}

func (w *Worker) finish(ctx context.Context, claim store.OutboxClaim, result channels.SendResult) error {
	now := w.options.Now()
	if result.Outcome == channels.OutcomeFailed && result.Retryable && claim.Attempts >= w.options.MaxAttempts {
		result.Retryable = false
	}
	return w.store.FinishOutbox(ctx, claim, result, now, now.Add(Backoff(claim.Attempts)))
}

func (w *Worker) deliver(ctx context.Context, claim store.OutboxClaim) error {
	if claim.Attempts > w.options.MaxAttempts {
		return w.finish(ctx, claim, channels.SendResult{Outcome: channels.OutcomeFailed, Code: store.DeliveryCodeGaveUp,
			Reason: fmt.Sprintf("Gave up after %d attempts.", w.options.MaxAttempts)})
	}
	sender := w.sender(claim.Type)
	if sender == nil {
		return w.finish(ctx, claim, channels.Failed(fmt.Sprintf("This server does not send replies through %s channels.", claim.Type)))
	}
	out, err := w.store.LoadOutbound(ctx, claim)
	if err != nil {
		return errors.Join(err, w.finish(ctx, claim, channels.Failed("The reply could not be loaded for sending.")))
	}
	adapter, registered := w.store.Channels().Adapter(claim.Type)
	singleton := registered && adapter.Definition().Singleton
	switch {
	case !out.Channel.Live() && !singleton:
		return w.finish(ctx, claim, channels.SendResult{Outcome: channels.OutcomeFailed, Code: store.DeliveryCodeChannelRemoved,
			Reason: out.Channel.Name + " was removed, so this reply cannot be sent."})
	case out.Channel.Live() && !out.Channel.Enabled:
		return w.finish(ctx, claim, channels.SendResult{Outcome: channels.OutcomeFailed, Code: store.DeliveryCodeChannelOff,
			Reason: out.Channel.Name + " is off: this reply is saved but not sent until the channel is on."})
	}
	result := sender.Send(ctx, out, kit{worker: w, workspaceID: claim.WorkspaceID, channelID: claim.ChannelID})
	if result.Outcome == channels.OutcomeFailed {
		w.options.Logger.Warn("reply delivery failed", "workspace", claim.WorkspaceID, "message", claim.MessageID, "type", claim.Type,
			"attempts", claim.Attempts, "retry", result.Retryable, "error", result.Reason)
	}
	return w.finish(ctx, claim, result)
}

// kit is what a sender gets from the worker.
type kit struct {
	worker                 *Worker
	workspaceID, channelID string
}

func (k kit) Now() time.Time { return k.worker.options.Now() }

func (k kit) Host() any { return k.worker.store }

func (k kit) OpenFile(ctx context.Context, key string) (io.ReadCloser, error) {
	storage := k.worker.store.Objects()
	if storage == nil {
		return nil, errors.New("attachment storage is not configured")
	}
	return storage.Open(ctx, key)
}

func (k kit) Credential(ctx context.Context, name string) (string, int64, bool, error) {
	return k.worker.store.OpenChannelCredential(ctx, k.workspaceID, k.channelID, name)
}

func (k kit) ReplaceCredential(ctx context.Context, name, value string, version int64, expiresAt *time.Time) (bool, error) {
	return k.worker.store.ReplaceChannelCredential(ctx, k.workspaceID, k.channelID, name, value, version, expiresAt)
}

func (k kit) RefreshCredential(ctx context.Context, name string, seen int64, refresh channels.Refresh) (string, int64, error) {
	return k.worker.store.RefreshChannelCredential(ctx, k.workspaceID, k.channelID, name, seen, refresh)
}
