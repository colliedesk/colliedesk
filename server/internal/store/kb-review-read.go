package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbReviewSelect reads reviews r with the names of the people on them; a caller adds the WHERE clause,
// whose $1 is the workspace.
const kbReviewSelect = `SELECT r.id::text,r.article_id::text,COALESCE(t.title,a.title),r.locale,r.state,r.note,r.content_hash,
	r.requested_by_agent_id::text,(SELECT ` + memberName + ` FROM agents a WHERE a.workspace_id=r.workspace_id AND a.id=r.requested_by_agent_id),
	r.requested_by_token_id::text,r.requested_by_token_label,
	r.decided_by_agent_id::text,(SELECT ` + memberName + ` FROM agents a WHERE a.workspace_id=r.workspace_id AND a.id=r.decided_by_agent_id),
	r.decided_by_token_id::text,r.decided_by_token_label,r.decided_at,r.decision_note,r.supersedes_review_id::text,r.created_at
	FROM kb_reviews r JOIN kb_articles a ON a.workspace_id=r.workspace_id AND a.id=r.article_id
	LEFT JOIN kb_article_translations t ON t.workspace_id=r.workspace_id AND t.article_id=r.article_id AND t.locale=r.locale
	WHERE r.workspace_id=$1 `

// readKBReview reads one review of the workspace, or domain.ErrKBReviewNotFound.
func readKBReview(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBReview, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBReview{}, domain.ErrKBReviewNotFound
	}
	reviews, err := queryKBReviews(ctx, tx, workspaceID, `AND r.id=$2`, id)
	if err != nil {
		return domain.KBReview{}, err
	}
	if len(reviews) == 0 {
		return domain.KBReview{}, domain.ErrKBReviewNotFound
	}
	return reviews[0], nil
}

// queryKBReviews reads the reviews matching where, with their reviewers, and tells for each whether
// its text is still the one the article or translation has unpublished (Current).
func queryKBReviews(ctx context.Context, tx pgx.Tx, workspaceID, where string, args ...any) ([]domain.KBReview, error) {
	rows, err := tx.Query(ctx, kbReviewSelect+where, append([]any{workspaceID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base reviews: %w", err)
	}
	reviews, err := pgx.CollectRows(rows, scanKBReview)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base reviews: %w", err)
	}
	hashes := map[string]string{}
	for i := range reviews {
		review := &reviews[i]
		if review.Reviewers, err = readKBReviewers(ctx, tx, workspaceID, review.ID); err != nil {
			return nil, err
		}
		key := review.ArticleID + "/" + deref(review.Locale)
		hash, seen := hashes[key]
		if !seen {
			if hash, err = kbPendingHash(ctx, tx, workspaceID, review.ArticleID, review.Locale); err != nil {
				return nil, err
			}
			hashes[key] = hash
		}
		review.Current = hash == review.ContentHash
	}
	return reviews, nil
}

func scanKBReview(row pgx.CollectableRow) (domain.KBReview, error) {
	var review domain.KBReview
	var requesterID, requesterName, requestTokenID, requestTokenLabel, deciderID, deciderName, decideTokenID, decideTokenLabel *string
	var decidedAt *time.Time
	if err := row.Scan(&review.ID, &review.ArticleID, &review.ArticleTitle, &review.Locale, &review.State, &review.Note, &review.ContentHash,
		&requesterID, &requesterName, &requestTokenID, &requestTokenLabel, &deciderID, &deciderName, &decideTokenID, &decideTokenLabel,
		&decidedAt, &review.DecisionNote, &review.SupersedesReviewID, &review.CreatedAt); err != nil {
		return review, err
	}
	review.CreatedAt = domain.UTC(review.CreatedAt)
	if decidedAt != nil {
		at := domain.UTC(*decidedAt)
		review.DecidedAt = &at
	}
	review.RequestedBy, review.DecidedBy = savedReplyPerson(requesterID, requesterName), savedReplyPerson(deciderID, deciderName)
	if requestTokenLabel != nil {
		review.RequestedByToken = &domain.SavedReplyToken{ID: requestTokenID, Label: *requestTokenLabel}
	}
	if decideTokenLabel != nil {
		review.DecidedByToken = &domain.SavedReplyToken{ID: decideTokenID, Label: *decideTokenLabel}
	}
	return review, nil
}

func readKBReviewers(ctx context.Context, tx pgx.Tx, workspaceID, reviewID string) ([]domain.KBReviewer, error) {
	rows, err := tx.Query(ctx, `SELECT r.agent_id::text,`+memberName+`,r.seen_at FROM kb_review_reviewers r JOIN agents a ON a.workspace_id=r.workspace_id AND a.id=r.agent_id
		WHERE r.workspace_id=$1 AND r.review_id=$2 ORDER BY 2,1`, workspaceID, reviewID)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base reviewers: %w", err)
	}
	reviewers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBReviewer, error) {
		var reviewer domain.KBReviewer
		err := row.Scan(&reviewer.AgentID, &reviewer.Name, &reviewer.SeenAt)
		if reviewer.SeenAt != nil {
			at := domain.UTC(*reviewer.SeenAt)
			reviewer.SeenAt = &at
		}
		return reviewer, err
	})
	if err != nil {
		return nil, fmt.Errorf("read knowledge base reviewers: %w", err)
	}
	return reviewers, nil
}

// kbPendingHash is the hash of the text a review of the article (locale nil) would be about now: its
// draft over the article. An article in the trash has none.
func kbPendingHash(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string) (string, error) {
	article, err := readKBArticle(ctx, tx, workspaceID, articleID)
	if err != nil {
		return "", err
	}
	return article.AsDrafted().Content().Hash(), nil
}
