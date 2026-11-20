package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbTextChangeKinds are the versions in which someone wrote the article's text: its author, for the
// four-eyes rule. A draft is one; a review event, a deletion or a move to and from the trash is not.
const kbTextChangeKinds = `('` + domain.KBArticleCreated + `','` + domain.KBArticleEdited + `','` + domain.KBArticleRestored + `','` +
	domain.KBArticleDrafted + `','` + domain.KBArticleImported + `','` + domain.KBArticleMediaReplaced + `')`

// kbTextAuthors returns those of people who wrote part of the unpublished text of the article (locale
// nil) or its translation: a version of a kbTextChangeKinds kind they made (themselves or through a
// token) after the last version that put the text on the help center. They never review it.
func kbTextAuthors(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, people []string) ([]string, error) {
	if locale != nil {
		return kbTranslationTextAuthors(ctx, tx, workspaceID, articleID, *locale, people)
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT v.changed_by_agent_id::text FROM kb_article_versions v WHERE v.workspace_id=$1 AND v.article_id=$2
		AND v.changed_by_agent_id=ANY($3::uuid[]) AND v.kind IN `+kbTextChangeKinds+` AND v.version > COALESCE((SELECT max(p.version) FROM kb_article_versions p
			WHERE p.workspace_id=$1 AND p.article_id=$2 AND p.status='published' AND `+kbTextVersion("p.")+`),0)`,
		workspaceID, articleID, people)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base text authors: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// settleKBReviewAfterEdit runs after every write of the text of an article (locale nil) or of its
// translation. An open review of a text that is no longer the pending one is superseded; while there
// is still something to review, a new review of the new text opens for the same reviewers, except the
// person who made the edit, and keeps who sent it. A published article without a draft, a published
// translation, or one in the trash, has nothing left to review.
func settleKBReviewAfterEdit(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID string, locale *string) error {
	var reviewID, hash string
	err := tx.QueryRow(ctx, `SELECT id::text,content_hash FROM kb_reviews WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3 AND state='open' FOR UPDATE`,
		workspaceID, articleID, locale).Scan(&reviewID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read open knowledge base review: %w", err)
	}
	pending, newHash, err := kbPendingText(ctx, tx, workspaceID, articleID, locale)
	if err != nil {
		return err
	}
	if pending && newHash == hash {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_reviews r SET state='superseded',decided_at=clock_timestamp(),decided_by_agent_id=$3,decided_by_token_id=t.id,decided_by_token_label=t.label
		FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$4 WHERE r.workspace_id=$1 AND r.id=$2`,
		workspaceID, reviewID, optionalID(actor.AgentID), optionalID(actor.TokenID)); err != nil {
		return fmt.Errorf("supersede knowledge base review: %w", err)
	}
	if !pending {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT agent_id::text FROM kb_review_reviewers WHERE workspace_id=$1 AND review_id=$2 AND agent_id IS DISTINCT FROM $3::uuid ORDER BY agent_id`,
		workspaceID, reviewID, optionalID(actor.AgentID))
	if err != nil {
		return err
	}
	reviewers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(reviewers) == 0 {
		return err
	}
	var next string
	if err := tx.QueryRow(ctx, `INSERT INTO kb_reviews(workspace_id,article_id,locale,content_hash,note,requested_by_agent_id,requested_by_token_id,requested_by_token_label,supersedes_review_id)
		SELECT workspace_id,article_id,locale,$3,note,requested_by_agent_id,requested_by_token_id,requested_by_token_label,id FROM kb_reviews WHERE workspace_id=$1 AND id=$2
		RETURNING id::text`, workspaceID, reviewID, newHash).Scan(&next); err != nil {
		return fmt.Errorf("reopen knowledge base review: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_review_reviewers(workspace_id,review_id,agent_id) SELECT $1,$2,unnest($3::uuid[])`, workspaceID, next, reviewers); err != nil {
		return fmt.Errorf("reopen knowledge base review: %w", err)
	}
	return nil
}
