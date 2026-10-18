// Package providerdomain deletes email provider domains nobody uses any more. A workspace that changes its
// support domain, disconnects email or is deleted queues the provider domain it held; this sweep removes it
// at the provider, because the provider account has a fixed number of domain slots shared by every workspace.
package providerdomain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	batchSize      = 20
	firstBackoff   = time.Minute
	maxBackoff     = time.Hour
	maxErrorLength = 300
	defaultDelay   = 250 * time.Millisecond
)

// Deleter removes one domain at the provider; a domain that is already gone is not an error.
type Deleter interface {
	DeleteDomain(ctx context.Context, id string) error
}

// Options tune the sweeper; zero values use the defaults.
type Options struct {
	Now    func() time.Time
	Logger *slog.Logger
	// Delay is the pause between two provider calls; zero uses 250 ms.
	Delay time.Duration
}

// Sweeper drains the queue of provider domains to delete.
type Sweeper struct {
	store   *store.Store
	domains Deleter
	options Options
}

// New builds a sweeper over the store and the provider client.
func New(st *store.Store, domains Deleter, options Options) *Sweeper {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Delay == 0 {
		options.Delay = defaultDelay
	}
	return &Sweeper{store: st, domains: domains, options: options}
}

// Backoff is the wait before the next try after the given number of failed attempts: 1 minute, doubling to 1 hour.
func Backoff(attempts int) time.Duration {
	delay := firstBackoff
	for range max(attempts, 0) {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}

// Sweep deletes every due domain once. A failure is kept with its error and retried after a backoff; the
// first error of the pass is returned after the rest were tried.
func (s *Sweeper) Sweep(ctx context.Context) error {
	var errs []error
	if _, err := s.store.ExpireUnverifiedProviderDomains(ctx, s.options.Now().Add(-domain.ProviderDomainUnverifiedTTL), s.options.Now()); err != nil {
		errs = append(errs, err)
	}
	for {
		now := s.options.Now()
		due, err := s.store.ClaimProviderDomainDeletions(ctx, now, batchSize)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		settled := 0
		for _, item := range due {
			if err := s.pause(ctx); err != nil {
				return errors.Join(append(errs, err)...)
			}
			if err := s.domains.DeleteDomain(ctx, item.ProviderDomainID); err != nil {
				s.options.Logger.Warn("delete unused email provider domain", "workspace", item.WorkspaceID, "domain", item.ProviderDomainID, "attempts", item.Attempts+1, "error", err)
				reason := err.Error()
				if len(reason) > maxErrorLength {
					reason = reason[:maxErrorLength]
				}
				if markErr := s.store.ProviderDomainDeleteFailed(ctx, item, reason, now.Add(Backoff(item.Attempts))); markErr != nil {
					errs = append(errs, markErr)
				}
				errs = append(errs, fmt.Errorf("delete provider domain %s: %w", item.ProviderDomainID, err))
				continue
			}
			if err := s.store.ProviderDomainDeleted(ctx, item); err != nil {
				errs = append(errs, err)
				continue
			}
			settled++
		}
		if len(due) < batchSize || settled == 0 {
			return errors.Join(errs...)
		}
	}
}

func (s *Sweeper) pause(ctx context.Context) error {
	timer := time.NewTimer(s.options.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run sweeps every interval until the context ends.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// Failures are already logged per domain with its attempt count.
		_ = s.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
