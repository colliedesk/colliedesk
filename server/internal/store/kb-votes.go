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
// brought in with the article and its published translations (Freshdesk's thumbs): a draft or a
// translation in the trash shows no reader, so its votes do not count. Who may read the article does
// not change its count; only a published article has a page that shows it. Imported votes count only
// while the article's votes were never reset.
var kbVoteCountsSQL = `SELECT
	(SELECT count(*) FROM kb_article_votes v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.reset_id IS NULL AND v.helpful),
	(SELECT count(*) FROM kb_article_votes v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.reset_id IS NULL AND NOT v.helpful),
	COALESCE(sum(` + importedThumb("thumbs_up") + `), 0)::bigint, COALESCE(sum(` + importedThumb("thumbs_down") + `), 0)::bigint
	FROM (SELECT a.source_meta FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2
		UNION ALL SELECT t.source_meta FROM kb_article_translations t
			WHERE t.workspace_id=$1 AND t.article_id=$2 AND ` + publishedKBTranslation + `) s(m)
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

// A reader the article has not counted yet is counted only while their address (the keyed hash the
// vote carries) added fewer than kbVoteReadersPerAddress readers to the article in kbVoteAddressWindow.
// Votes a reset closed still count here, so a reset does not reopen the address.
const (
	kbVoteReadersPerAddress = 10
	kbVoteAddressWindow     = "24 hours"
)

// lockKBVotes locks the votes version of article articleID, making the row on the first vote: the votes
// and resets of one article take their turns on it, so the readers an address added are counted
// without a race and a reset counts or closes every vote. Other articles' votes never wait. One
// statement makes the row or locks the one there (ON CONFLICT DO SELECT FOR UPDATE).
func lockKBVotes(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_vote_versions (workspace_id, article_id) VALUES ($1, $2)
		ON CONFLICT (workspace_id, article_id) DO SELECT FOR UPDATE RETURNING 1`, workspaceID, articleID); err != nil {
		return fmt.Errorf("lock votes version: %w", err)
	}
	return nil
}

// bumpKBVotes tells the cached page of article articleID that its count changed, after lockKBVotes.
// The knowledge base's content_version and the other articles' pages stay.
func bumpKBVotes(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) error {
	if _, err := tx.Exec(ctx, `UPDATE kb_article_vote_versions SET version=version+1 WHERE workspace_id=$1 AND article_id=$2`, workspaceID, articleID); err != nil {
		return fmt.Errorf("bump votes version: %w", err)
	}
	return nil
}

// KBArticleVotesVersion is the votes version of the article number of workspaceID on help center
// helpCenterID ("" the primary one), part of its page's ETag: 0 before its first vote, and for an
// article that help center does not show.
func (s *Store) KBArticleVotesVersion(ctx context.Context, workspaceID, helpCenterID string, number int64) (int64, error) {
	return helpCenterScoped(ctx, s, workspaceID, helpCenterID, func(tx pgx.Tx, _ string) (int64, error) {
		var version int64
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT v.version FROM kb_article_vote_versions v JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.article_id
			WHERE v.workspace_id=$1 AND a.number=$2), 0)`, workspaceID, number).Scan(&version)
		if err != nil {
			return 0, fmt.Errorf("read votes version: %w", err)
		}
		return version, nil
	})
}

// VoteKBArticle records the vote of one reader (voter, the SHA-256 of their id) on the published article
// number of workspaceID: a second vote replaces the first. address is the keyed hash of the address the
// vote came from; a new reader past kbVoteReadersPerAddress of it is not counted, with no error, so the
// page thanks them like any reader. A "No" may carry missing, the reader's "What was missing?" line,
// kept masked (textmask) and cut to MaxKBVoteMissingChars; an empty one keeps the line the vote had. A
// "Yes" has none. domain.ErrKBArticleNotFound when help center helpCenterID ("" the primary one) shows
// no such article.
func (s *Store) VoteKBArticle(ctx context.Context, workspaceID, helpCenterID string, number int64, voter, address []byte, helpful bool, missing string) error {
	missing = maskedKBVoteLine(missing)
	if helpful {
		missing = ""
	}
	_, err := helpCenterScoped(ctx, s, workspaceID, helpCenterID, func(tx pgx.Tx, helpCenterID string) (struct{}, error) {
		var articleID string
		query, args := onHelpCenter(helpCenterID, `SELECT a.id::text FROM kb_articles a WHERE `+shownKBArticle+` AND a.number=$2`, workspaceID, number)
		err := tx.QueryRow(ctx, query, args...).Scan(&articleID)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrKBArticleNotFound
		}
		if err != nil {
			return struct{}{}, fmt.Errorf("find article to vote on: %w", err)
		}
		if err := lockKBVotes(ctx, tx, workspaceID, articleID); err != nil {
			return struct{}{}, err
		}
		var full bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM kb_article_votes WHERE workspace_id=$1 AND article_id=$2 AND voter=$3 AND reset_id IS NULL)
			AND (SELECT count(*) FROM kb_article_votes WHERE workspace_id=$1 AND article_id=$2 AND address=$4
				AND created_at > now() - $5::interval) >= $6`,
			workspaceID, articleID, voter, address, kbVoteAddressWindow, kbVoteReadersPerAddress).Scan(&full); err != nil {
			return struct{}{}, fmt.Errorf("count readers of the address: %w", err)
		}
		if full {
			return struct{}{}, nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kb_article_votes (workspace_id, article_id, voter, address, helpful, missing) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
			ON CONFLICT (workspace_id, article_id, voter) WHERE reset_id IS NULL DO UPDATE SET helpful=EXCLUDED.helpful,
				missing=CASE WHEN EXCLUDED.helpful THEN NULL ELSE COALESCE(EXCLUDED.missing, kb_article_votes.missing) END, updated_at=now()`,
			workspaceID, articleID, voter, address, helpful, missing); err != nil {
			return struct{}{}, fmt.Errorf("record article vote: %w", err)
		}
		return struct{}{}, bumpKBVotes(ctx, tx, workspaceID, articleID)
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
