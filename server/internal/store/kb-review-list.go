package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// maxKBReviewList bounds one list of reviews; a filter narrows a longer one.
const maxKBReviewList = 200

var kbReviewStates = []string{domain.KBReviewOpen, domain.KBReviewApproved, domain.KBReviewChangesRequested, domain.KBReviewWithdrawn, domain.KBReviewSuperseded}

// ListKBReviews lists the workspace's reviews matching filter, newest first.
func (s *Store) ListKBReviews(ctx context.Context, workspaceID string, filter domain.KBReviewFilter) ([]domain.KBReview, error) {
	where, args := "", []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(clause, "$"+strconv.Itoa(len(args)+1))
	}
	if filter.State != "" {
		if !slices.Contains(kbReviewStates, filter.State) {
			return nil, &domain.InvalidKBError{Message: "state is one of open, approved, changes_requested, withdrawn or superseded."}
		}
		add(`AND r.state=%s `, filter.State)
	}
	for _, id := range []struct{ value, clause, name string }{
		{filter.ReviewerID, `AND EXISTS (SELECT 1 FROM kb_review_reviewers x WHERE x.workspace_id=r.workspace_id AND x.review_id=r.id AND x.agent_id=%s::uuid) `, "reviewer"},
		{filter.ArticleID, `AND r.article_id=%s::uuid `, "article"},
	} {
		if id.value == "" {
			continue
		}
		if !savedReplyID.MatchString(id.value) {
			if id.name == "article" {
				return nil, domain.ErrKBArticleNotFound
			}
			return []domain.KBReview{}, nil
		}
		add(id.clause, id.value)
	}
	if filter.ArticleID != "" {
		// One article's list holds its own reviews, or those of the translation filter.Locale names.
		if filter.Locale != nil {
			if err := translationKey(filter.ArticleID, *filter.Locale); err != nil {
				return nil, err
			}
		}
		add(`AND r.locale IS NOT DISTINCT FROM %s::text `, filter.Locale)
	}
	reviews, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBReview, error) {
		if filter.ArticleID != "" {
			if _, err := readKBArticle(ctx, tx, workspaceID, filter.ArticleID); err != nil {
				return nil, err
			}
		}
		if filter.Locale != nil {
			if _, err := readKBTranslation(ctx, tx, workspaceID, filter.ArticleID, *filter.Locale); err != nil {
				return nil, err
			}
		}
		return queryKBReviews(ctx, tx, workspaceID, where+`ORDER BY r.created_at DESC,r.id DESC LIMIT `+strconv.Itoa(maxKBReviewList), args...)
	})
	return reviews, kbError("list knowledge base reviews", err)
}

// GetKBReview reads one review with the text it is about and the line diff against the live text. A
// reviewer who reads it is marked as having seen it.
func (s *Store) GetKBReview(ctx context.Context, workspaceID string, actor Actor, reviewID string) (domain.KBReviewDetail, error) {
	detail, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBReviewDetail, error) {
		if !savedReplyID.MatchString(reviewID) {
			return domain.KBReviewDetail{}, domain.ErrKBReviewNotFound
		}
		if actor.AgentID != "" {
			if _, err := tx.Exec(ctx, `UPDATE kb_review_reviewers SET seen_at=clock_timestamp() WHERE workspace_id=$1 AND review_id=$2 AND agent_id=$3 AND seen_at IS NULL`,
				workspaceID, reviewID, actor.AgentID); err != nil {
				return domain.KBReviewDetail{}, fmt.Errorf("mark knowledge base review seen: %w", err)
			}
		}
		review, err := readKBReview(ctx, tx, workspaceID, reviewID)
		if err != nil {
			return domain.KBReviewDetail{}, err
		}
		proposed, current, err := reviewedKBText(ctx, tx, workspaceID, review)
		if err != nil {
			return domain.KBReviewDetail{}, err
		}
		detail := domain.KBReviewDetail{KBReview: review, Proposed: proposed, Live: current}
		live := ""
		if current != nil {
			live = current.BodyMD
		}
		detail.Diff = domain.LineDiff(live, detail.Proposed.BodyMD)
		return detail, nil
	})
	return detail, kbError("read knowledge base review", err)
}
