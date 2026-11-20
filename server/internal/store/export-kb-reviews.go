package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportKBReviews writes every review of an article or a translation, decided and open alike, then
// every person asked to review one: a restored workspace still knows who approved which text.
func exportKBReviews(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,article_id::text,locale,content_hash,state,note,requested_by_agent_id::text,requested_by_token_id::text,
		requested_by_token_label,decided_by_agent_id::text,decided_by_token_id::text,decided_by_token_label,decided_at,decision_note,supersedes_review_id::text,
		approves_pending,author_email,created_at
		FROM kb_reviews WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base reviews: %w", err)
	}
	for rows.Next() {
		var record export.KBReview
		if err := rows.Scan(&record.ID, &record.ArticleID, &record.Locale, &record.ContentHash, &record.State, &record.Note, &record.RequestedByAgentID,
			&record.RequestedByTokenID, &record.RequestedByTokenLabel, &record.DecidedByAgentID, &record.DecidedByTokenID, &record.DecidedByTokenLabel,
			&record.DecidedAt, &record.DecisionNote, &record.SupersedesReviewID, &record.ApprovesPending, &record.AuthorEmail, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base reviews: %w", err)
		}
		record.CreatedAt, record.DecidedAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.DecidedAt)
		if err := sink.KBReview(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base reviews", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT review_id::text,agent_id::text,email,notified_at,seen_at,created_at
		FROM kb_review_reviewers WHERE workspace_id=$1 ORDER BY created_at, review_id, agent_id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base reviewers: %w", err)
	}
	for rows.Next() {
		var record export.KBReviewReviewer
		if err := rows.Scan(&record.ReviewID, &record.AgentID, &record.Email, &record.NotifiedAt, &record.SeenAt, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base reviewers: %w", err)
		}
		record.CreatedAt, record.NotifiedAt, record.SeenAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.NotifiedAt), domain.UTCOptional(record.SeenAt)
		if err := sink.KBReviewReviewer(record); err != nil {
			rows.Close()
			return err
		}
	}
	return rowsError("export knowledge base reviewers", rows)
}
