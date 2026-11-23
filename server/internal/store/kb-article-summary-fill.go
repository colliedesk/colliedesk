package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// fillKBSummaries sets what scanKBSummary leaves out of articles: their collections, tags, translations,
// author, review state and open conflicts. One query each for the whole page.
func fillKBSummaries(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	for _, fill := range []func(context.Context, pgx.Tx, string, ...*domain.KBArticleSummary) error{
		fillKBCollectionIDs, fillKBTags, fillKBLanguages, fillKBAuthors, fillKBReviewStates, fillKBOpenConflicts,
	} {
		if err := fill(ctx, tx, workspaceID, items...); err != nil {
			return err
		}
	}
	return nil
}

// fillKBReviewStates sets the ReviewState of articles: KBInReview while their own unpublished text waits
// for a reviewer, KBApproved once a reviewer approved it as it is now, nil otherwise.
func fillKBReviewStates(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	ids := make([]string, 0, len(items))
	byID := make(map[string]*domain.KBArticleSummary, len(items))
	for _, item := range items {
		item.ReviewState = nil
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT article_id::text, bool_or(state='open'), bool_or(approves_pending) FROM kb_reviews
		WHERE workspace_id=$1 AND article_id=ANY($2::uuid[]) AND locale IS NULL AND (state='open' OR approves_pending)
		GROUP BY article_id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read knowledge base article reviews: %w", err)
	}
	var articleID string
	var open, approved bool
	if _, err := pgx.ForEachRow(rows, []any{&articleID, &open, &approved}, func() error {
		item := byID[articleID]
		switch {
		case item == nil:
		case open:
			state := KBInReview
			item.ReviewState = &state
		case approved:
			state := KBApproved
			item.ReviewState = &state
		}
		return nil
	}); err != nil {
		return fmt.Errorf("read knowledge base article reviews: %w", err)
	}
	return nil
}
