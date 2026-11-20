package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBReviewDecision is what a person does with an open review: approve it, ask for changes (Note says
// which), or withdraw it.
type KBReviewDecision struct {
	State string
	Note  string
	// WithdrawAny lets a person who may publish withdraw a review someone else sent.
	WithdrawAny bool
}

// DecideKBReview closes an open review. Only a person it names decides it, never the one who sent it
// nor one of their agent tokens: a token acts for the person who made it. A withdrawal is the sender's,
// or of anyone who may publish.
func (s *Store) DecideKBReview(ctx context.Context, workspaceID string, actor Actor, reviewID string, decision KBReviewDecision) (domain.KBReview, error) {
	note := strings.TrimSpace(decision.Note)
	if len([]rune(note)) > domain.MaxKBReviewNote || strings.ContainsAny(note, "\r\n") {
		return domain.KBReview{}, &domain.InvalidKBError{Message: fmt.Sprintf("The note is one line of at most %d characters.", domain.MaxKBReviewNote)}
	}
	kind, ok := map[string]string{domain.KBReviewApproved: domain.KBReviewApprovedKind, domain.KBReviewChangesRequested: domain.KBReviewChangesKind,
		domain.KBReviewWithdrawn: domain.KBReviewWithdrawnKind}[decision.State]
	if !ok {
		return domain.KBReview{}, fmt.Errorf("decide knowledge base review: unknown decision %q", decision.State)
	}
	if decision.State == domain.KBReviewChangesRequested && note == "" {
		return domain.KBReview{}, &domain.InvalidKBError{Message: "Say in note, in one line, what to change."}
	}
	review, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBReview, error) {
		review, err := readKBReview(ctx, tx, workspaceID, reviewID)
		if err != nil {
			return review, err
		}
		if _, _, err := lockKBArticleForReview(ctx, tx, workspaceID, review.ArticleID); err != nil {
			return review, err
		}
		if review, err = lockedKBReview(ctx, tx, workspaceID, reviewID); err != nil {
			return review, err
		}
		if err := mayDecideKBReview(review, actor, decision); err != nil {
			return review, err
		}
		if decision.State != domain.KBReviewWithdrawn {
			// A reviewer who wrote part of the text since was taken off the review; this holds for one
			// who edited it some other way too, as an import.
			authors, err := kbTextAuthors(ctx, tx, workspaceID, review.ArticleID, review.Locale, []string{actor.AgentID})
			if err != nil {
				return review, err
			}
			if len(authors) > 0 {
				return review, domain.ErrKBOwnReview
			}
		}
		if err := addKBReviewVersion(ctx, tx, workspaceID, review.ArticleID, review.Locale, actor, kind); err != nil {
			return review, err
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_reviews r SET state=$3,decision_note=$4,decided_by_agent_id=$5,decided_by_token_id=t.id,decided_by_token_label=t.label,
				decided_at=clock_timestamp(),author_email=CASE WHEN $3<>'withdrawn' AND r.requested_by_agent_id IS NOT NULL THEN 'queued' ELSE 'none' END
			FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$6
			WHERE r.workspace_id=$1 AND r.id=$2`, workspaceID, reviewID, decision.State, note, optionalID(actor.AgentID), optionalID(actor.TokenID)); err != nil {
			return review, fmt.Errorf("decide knowledge base review: %w", err)
		}
		return readKBReview(ctx, tx, workspaceID, reviewID)
	})
	return review, kbError("decide knowledge base review", err)
}

// lockedKBReview reads the review again once its article is locked, so two decisions never both pass.
func lockedKBReview(ctx context.Context, tx pgx.Tx, workspaceID, reviewID string) (domain.KBReview, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM kb_reviews WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, reviewID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBReview{}, domain.ErrKBReviewNotFound
	}
	if err != nil {
		return domain.KBReview{}, err
	}
	return readKBReview(ctx, tx, workspaceID, reviewID)
}

// mayDecideKBReview applies the four-eyes rule: the review is open and still about the current text,
// and the person deciding (a token's maker, for a token) is a named reviewer who did not send it.
func mayDecideKBReview(review domain.KBReview, actor Actor, decision KBReviewDecision) error {
	if review.State != domain.KBReviewOpen || !review.Current {
		return domain.ErrKBReviewClosed
	}
	requester := review.RequestedBy != nil && review.RequestedBy.AgentID != nil && *review.RequestedBy.AgentID == actor.AgentID
	if decision.State == domain.KBReviewWithdrawn {
		if requester || decision.WithdrawAny {
			return nil
		}
		return domain.ErrKBNotAReviewer
	}
	if requester || actor.AgentID == "" {
		return domain.ErrKBOwnReview
	}
	for _, reviewer := range review.Reviewers {
		if reviewer.AgentID == actor.AgentID {
			return nil
		}
	}
	return domain.ErrKBNotAReviewer
}
