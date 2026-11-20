package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBReviewEmailDue is a review email waiting to go: to the reviewer AgentID, or, with AgentID empty, to
// the person who sent the text for review, telling them it was decided.
type KBReviewEmailDue struct{ WorkspaceID, ReviewID, AgentID string }

// DueKBReviewEmails lists, across workspaces, the review emails to send now (queued, or left sending by a
// stopped server), oldest first.
func (s *Store) DueKBReviewEmails(ctx context.Context, abandoned time.Duration, limit int) ([]KBReviewEmailDue, error) {
	var due []KBReviewEmailDue
	err := s.db.Privileged(ctx, database.PrivilegeKBReviewEmailClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,review_id::text,agent_id::text,created_at FROM kb_review_reviewers
				WHERE email='queued' OR (email='sending' AND email_claimed_at<now()-make_interval(secs => $1))
			UNION ALL SELECT workspace_id::text,id::text,'',decided_at FROM kb_reviews
				WHERE author_email='queued' OR (author_email='sending' AND author_email_claimed_at<now()-make_interval(secs => $1))
			ORDER BY 4 LIMIT $2`, abandoned.Seconds(), limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (KBReviewEmailDue, error) {
			var item KBReviewEmailDue
			var at time.Time
			return item, row.Scan(&item.WorkspaceID, &item.ReviewID, &item.AgentID, &at)
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list due knowledge base review emails: %w", err)
	}
	return due, nil
}

// KBReviewEmail is what one review email is about, and to whom.
type KBReviewEmail struct {
	KBReviewEmailDue
	To, Slug, WorkspaceName string
	Review                  domain.KBReview
}

// ClaimKBReviewEmail claims one review email right before it goes, unless another server is sending it or
// it went. A reviewer deactivated since, or asked about a review that is no longer open, gets none; nor
// does an author deactivated or removed since.
func (s *Store) ClaimKBReviewEmail(ctx context.Context, due KBReviewEmailDue, abandoned time.Duration) (KBReviewEmail, bool, error) {
	mail := KBReviewEmail{KBReviewEmailDue: due}
	claimed, err := scoped(ctx, s, due.WorkspaceID, func(tx pgx.Tx) (bool, error) {
		wanted, err := claimKBReviewEmail(ctx, tx, &mail, abandoned)
		if errors.Is(err, pgx.ErrNoRows) {
			if due.AgentID == "" {
				// The author was removed (the review kept no person): there is nobody to tell.
				_, err := tx.Exec(ctx, `UPDATE kb_reviews SET author_email='off'
					WHERE workspace_id=$1 AND id=$2 AND author_email IN ('queued','sending') AND requested_by_agent_id IS NULL`, due.WorkspaceID, due.ReviewID)
				return false, err
			}
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("claim knowledge base review email: %w", err)
		}
		if !wanted {
			return false, settleKBReviewEmail(ctx, tx, due, "off")
		}
		mail.Review, err = readKBReview(ctx, tx, due.WorkspaceID, due.ReviewID)
		return err == nil, err
	})
	return mail, claimed, err
}

// claimKBReviewEmail marks the email sending and reads its address; false when it should not go.
func claimKBReviewEmail(ctx context.Context, tx pgx.Tx, mail *KBReviewEmail, abandoned time.Duration) (bool, error) {
	var active bool
	if mail.AgentID == "" {
		err := tx.QueryRow(ctx, `UPDATE kb_reviews r SET author_email='sending',author_email_claimed_at=now()
			FROM agents p, workspaces w
			WHERE r.workspace_id=$1 AND r.id=$2
				AND (r.author_email='queued' OR (r.author_email='sending' AND r.author_email_claimed_at<now()-make_interval(secs => $3)))
				AND p.workspace_id=r.workspace_id AND p.id=r.requested_by_agent_id AND w.id=r.workspace_id
			RETURNING p.email,p.deactivated_at IS NULL,w.slug,w.name`,
			mail.WorkspaceID, mail.ReviewID, abandoned.Seconds()).Scan(&mail.To, &active, &mail.Slug, &mail.WorkspaceName)
		return active, err
	}
	var state string
	err := tx.QueryRow(ctx, `UPDATE kb_review_reviewers x SET email='sending',email_claimed_at=now()
		FROM agents p, kb_reviews r, workspaces w
		WHERE x.workspace_id=$1 AND x.review_id=$2 AND x.agent_id=$3
			AND (x.email='queued' OR (x.email='sending' AND x.email_claimed_at<now()-make_interval(secs => $4)))
			AND p.workspace_id=x.workspace_id AND p.id=x.agent_id AND r.workspace_id=x.workspace_id AND r.id=x.review_id AND w.id=x.workspace_id
		RETURNING p.email,p.deactivated_at IS NULL,r.state,w.slug,w.name`,
		mail.WorkspaceID, mail.ReviewID, mail.AgentID, abandoned.Seconds()).Scan(&mail.To, &active, &state, &mail.Slug, &mail.WorkspaceName)
	return active && state == domain.KBReviewOpen, err
}

// SettleKBReviewEmail records how a review email ended: sent or rejected; "" gives the claim back for a
// later round (a send that failed for now). A reviewer's sent email records when they were told.
func (s *Store) SettleKBReviewEmail(ctx context.Context, due KBReviewEmailDue, status string) error {
	if status == "" {
		status = "queued"
	}
	_, err := scoped(ctx, s, due.WorkspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, settleKBReviewEmail(ctx, tx, due, status)
	})
	return err
}

func settleKBReviewEmail(ctx context.Context, tx pgx.Tx, due KBReviewEmailDue, status string) error {
	var err error
	if due.AgentID != "" {
		_, err = tx.Exec(ctx, `UPDATE kb_review_reviewers SET email=$4,notified_at=CASE WHEN $4='sent' THEN now() ELSE notified_at END
			WHERE workspace_id=$1 AND review_id=$2 AND agent_id=$3 AND email='sending'`, due.WorkspaceID, due.ReviewID, due.AgentID, status)
	} else {
		_, err = tx.Exec(ctx, `UPDATE kb_reviews SET author_email=$3 WHERE workspace_id=$1 AND id=$2 AND author_email='sending'`, due.WorkspaceID, due.ReviewID, status)
	}
	if err != nil {
		return fmt.Errorf("settle knowledge base review email: %w", err)
	}
	return nil
}
