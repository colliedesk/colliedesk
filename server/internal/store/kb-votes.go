package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/textmask"
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

// bumpKBVotes tells cached article pages of the workspace's help center that a count changed. The
// knowledge base's content_version stays: no article changed.
const bumpKBVotes = `UPDATE help_centers SET votes_version=votes_version+1 WHERE workspace_id=$1`

// VoteKBArticle records the vote of one reader (voter, the SHA-256 of their id) on the published article
// number of workspaceID: a second vote replaces the first. A "No" may carry missing, the reader's
// "What was missing?" line, kept masked (textmask) and cut to MaxKBVoteMissingChars; an empty one keeps
// the line the vote had. A "Yes" has none. domain.ErrKBArticleNotFound when no such article is shown.
func (s *Store) VoteKBArticle(ctx context.Context, workspaceID string, number int64, voter []byte, helpful bool, missing string) error {
	missing = maskedKBVoteLine(missing)
	if helpful {
		missing = ""
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		var articleID string
		// FOR SHARE: a reset (FOR UPDATE) of the article waits for the vote, so it counts it or closes it.
		err := tx.QueryRow(ctx, `SELECT a.id::text FROM kb_articles a WHERE `+publishedKBArticle+` AND a.number=$2 FOR SHARE OF a`, workspaceID, number).Scan(&articleID)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrKBArticleNotFound
		}
		if err != nil {
			return struct{}{}, fmt.Errorf("find article to vote on: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kb_article_votes (workspace_id, article_id, voter, helpful, missing) VALUES ($1, $2, $3, $4, NULLIF($5, ''))
			ON CONFLICT (workspace_id, article_id, voter) WHERE reset_id IS NULL DO UPDATE SET helpful=EXCLUDED.helpful,
				missing=CASE WHEN EXCLUDED.helpful THEN NULL ELSE COALESCE(EXCLUDED.missing, kb_article_votes.missing) END, updated_at=now()`,
			workspaceID, articleID, voter, helpful, missing); err != nil {
			return struct{}{}, fmt.Errorf("record article vote: %w", err)
		}
		if _, err := tx.Exec(ctx, bumpKBVotes, workspaceID); err != nil {
			return struct{}{}, fmt.Errorf("bump votes version: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

// maskedKBVoteLine is a reader's line as it is kept: masked whole, then cut to its longest length, so a
// cut never leaves part of an address or number behind.
func maskedKBVoteLine(line string) string {
	line = strings.TrimSpace(textmask.Mask(line))
	if runes := []rune(line); len(runes) > domain.MaxKBVoteMissingChars {
		line = strings.TrimSpace(string(runes[:domain.MaxKBVoteMissingChars]))
	}
	return line
}
