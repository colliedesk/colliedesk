package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// PausedWorkspace names the workspace a complaint pause just stopped, for the operator alert.
type PausedWorkspace struct {
	Name, Slug string
	Pause      domain.EmailPause
}

// PauseEmailOnComplaints pauses the workspace's email sending when the complaints Resend reported in
// the last domain.ComplaintPauseWindow are over the limits of domain.ComplaintRateExceeded, counted
// against the replies handed to the relay in the same window. It returns the pause only to the call
// that made it, so one pause means one operator alert; nil means nothing changed. A reply failed by a
// complaint keeps its sent_at, so it still counts as sent.
func (s *Store) PauseEmailOnComplaints(ctx context.Context, workspaceID string, now time.Time) (*PausedWorkspace, error) {
	paused, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*PausedWorkspace, error) {
		since := now.Add(-domain.ComplaintPauseWindow)
		var complaints, sent int
		var alreadyPaused bool
		err := tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM relay_webhook_events WHERE workspace_id=$1 AND event_type='email.complained' AND received_at>$2),
				(SELECT count(*) FROM email_outbox WHERE workspace_id=$1 AND sent_at>$2),
				(SELECT email_paused_at IS NOT NULL FROM workspaces WHERE id=$1)`, workspaceID, since).Scan(&complaints, &sent, &alreadyPaused)
		if err != nil {
			return nil, err
		}
		if alreadyPaused || !domain.ComplaintRateExceeded(complaints, sent) {
			return nil, nil
		}
		result := PausedWorkspace{Pause: domain.EmailPause{PausedAt: now.UTC(), Reason: fmt.Sprintf(
			"%d spam complaints on %d replies sent in the last %d days (%.2f%%).",
			complaints, sent, int(domain.ComplaintPauseWindow/(24*time.Hour)), 100*float64(complaints)/float64(max(sent, 1)))}}
		err = tx.QueryRow(ctx, `UPDATE workspaces SET email_paused_at=$2,email_pause_reason=$3
			WHERE id=$1 AND email_paused_at IS NULL RETURNING name,slug`, workspaceID, now, result.Pause.Reason).Scan(&result.Name, &result.Slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &result, nil
	})
	if err != nil {
		return nil, fmt.Errorf("pause email sending: %w", err)
	}
	return paused, nil
}

// GetEmailPause returns why the workspace's email sending is paused, or nil when it is not.
func (s *Store) GetEmailPause(ctx context.Context, workspaceID string) (*domain.EmailPause, error) {
	pause, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*domain.EmailPause, error) {
		var pausedAt *time.Time
		var reason *string
		if err := tx.QueryRow(ctx, `SELECT email_paused_at,email_pause_reason FROM workspaces WHERE id=$1`, workspaceID).Scan(&pausedAt, &reason); err != nil {
			return nil, err
		}
		if pausedAt == nil || reason == nil {
			return nil, nil
		}
		return &domain.EmailPause{PausedAt: pausedAt.UTC(), Reason: *reason}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get email pause: %w", err)
	}
	return pause, nil
}

// ResumeEmailSending lifts the pause and returns what it was, or nil when the workspace was not paused.
// Replies held back by the pause become due at once instead of waiting out their deferral.
func (s *Store) ResumeEmailSending(ctx context.Context, workspaceID string) (*domain.EmailPause, error) {
	resumed, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*domain.EmailPause, error) {
		var pause domain.EmailPause
		err := tx.QueryRow(ctx, `UPDATE workspaces w SET email_paused_at=NULL,email_pause_reason=NULL
			FROM (SELECT id,email_paused_at,email_pause_reason FROM workspaces WHERE id=$1 AND email_paused_at IS NOT NULL FOR UPDATE) old
			WHERE w.id=old.id RETURNING old.email_paused_at,old.email_pause_reason`, workspaceID).Scan(&pause.PausedAt, &pause.Reason)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE email_outbox SET next_attempt_at=LEAST(next_attempt_at,now()),updated_at=now()
			WHERE workspace_id=$1 AND status='queued'`, workspaceID); err != nil {
			return nil, err
		}
		pause.PausedAt = pause.PausedAt.UTC()
		return &pause, nil
	})
	if err != nil {
		return nil, fmt.Errorf("resume email sending: %w", err)
	}
	return resumed, nil
}
