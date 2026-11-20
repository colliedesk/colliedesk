package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// importedThumb reads a count an import stored in source_meta m under key: a number, else 0, never
// negative and capped so a sum of a few never overflows.
func importedThumb(key string) string {
	return `CASE WHEN jsonb_typeof(m->'` + key + `')='number' THEN LEAST(GREATEST(floor((m->>'` + key + `')::numeric), 0), 1000000000)::bigint ELSE 0 END`
}

// kbVoteCountsSQL reads, for article $2 of workspace $1, the current votes and the votes an import
// brought in with the article and its translations (Freshdesk's thumbs). Imported votes count only
// while the article's votes were never reset.
var kbVoteCountsSQL = `SELECT
	(SELECT count(*) FROM kb_article_votes v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.reset_id IS NULL AND v.helpful),
	(SELECT count(*) FROM kb_article_votes v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.reset_id IS NULL AND NOT v.helpful),
	COALESCE(sum(` + importedThumb("thumbs_up") + `), 0)::bigint, COALESCE(sum(` + importedThumb("thumbs_down") + `), 0)::bigint
	FROM (SELECT a.source_meta FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2
		UNION ALL SELECT t.source_meta FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2) s(m)
	WHERE m IS NOT NULL AND NOT EXISTS (SELECT 1 FROM kb_article_vote_resets r WHERE r.workspace_id=$1 AND r.article_id=$2)`

// kbVoteCounts are the current and imported counts of an article, read in tx.
func kbVoteCounts(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) (current, imported domain.KBVoteTally, err error) {
	var up, down, importedUp, importedDown int64
	if err := tx.QueryRow(ctx, kbVoteCountsSQL, workspaceID, articleID).Scan(&up, &down, &importedUp, &importedDown); err != nil {
		return current, imported, fmt.Errorf("count article votes: %w", err)
	}
	return domain.KBVoteTally{Helpful: int(up), NotHelpful: int(down)}, domain.KBVoteTally{Helpful: int(importedUp), NotHelpful: int(importedDown)}, nil
}

// publicKBVoteCount is what an article's page shows of its votes.
func publicKBVoteCount(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) (domain.KBVoteCount, error) {
	current, imported, err := kbVoteCounts(ctx, tx, workspaceID, articleID)
	helpful := current.Helpful + imported.Helpful
	return domain.KBVoteCount{Helpful: helpful, Total: helpful + current.NotHelpful + imported.NotHelpful}, err
}
