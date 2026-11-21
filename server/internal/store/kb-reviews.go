package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbPublishPermission is the permission a reviewer holds: approving a text is deciding it may go on the
// help center. (auth.PermKBArticlesPublish; auth imports this package.)
const kbPublishPermission = "kb_articles:publish"

// KBReviewRequest is who is asked to review a text, and the one line the author tells them.
type KBReviewRequest struct {
	ReviewerIDs []string
	Note        string
	// Locale names the translation to review; nil reviews the article.
	Locale *string
}

// RequestKBReview sends the article's unpublished text (its draft, or the article itself while it is
// not published), or a translation that is a draft, for review to people who may publish. The review
// holds the hash of that text.
func (s *Store) RequestKBReview(ctx context.Context, workspaceID string, actor Actor, articleID string, request KBReviewRequest) (domain.KBReview, error) {
	if !savedReplyID.MatchString(articleID) {
		return domain.KBReview{}, domain.ErrKBArticleNotFound
	}
	if request.Locale != nil {
		if err := translationKey(articleID, *request.Locale); err != nil {
			return domain.KBReview{}, err
		}
	}
	note, reviewers, err := checkKBReviewRequest(actor, request)
	if err != nil {
		return domain.KBReview{}, err
	}
	review, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBReview, error) {
		if _, _, err := lockKBArticleForReview(ctx, tx, workspaceID, articleID); err != nil {
			return domain.KBReview{}, err
		}
		pending, hash, err := kbPendingText(ctx, tx, workspaceID, articleID, request.Locale)
		if err != nil {
			return domain.KBReview{}, err
		}
		if !pending {
			return domain.KBReview{}, domain.ErrKBNothingToReview
		}
		if err := checkKBReviewers(ctx, tx, workspaceID, articleID, request.Locale, reviewers); err != nil {
			return domain.KBReview{}, err
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_reviews WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3 AND state='open')`,
			workspaceID, articleID, request.Locale).Scan(&open); err != nil {
			return domain.KBReview{}, err
		}
		if open {
			return domain.KBReview{}, domain.ErrKBReviewOpen
		}
		id, err := insertKBReview(ctx, tx, workspaceID, actor, articleID, request.Locale, hash, note, nil, reviewers)
		if err != nil {
			return domain.KBReview{}, err
		}
		if err := addKBReviewVersion(ctx, tx, workspaceID, articleID, request.Locale, actor, domain.KBReviewRequested); err != nil {
			return domain.KBReview{}, err
		}
		return readKBReview(ctx, tx, workspaceID, id)
	})
	return review, kbError("request knowledge base review", err)
}

// checkKBReviewRequest cleans the note and the reviewer list: one to MaxKBReviewers people, never the
// person who asks.
func checkKBReviewRequest(actor Actor, request KBReviewRequest) (string, []string, error) {
	note := strings.TrimSpace(request.Note)
	if len([]rune(note)) > domain.MaxKBReviewNote || strings.ContainsAny(note, "\r\n") {
		return "", nil, &domain.InvalidKBError{Message: fmt.Sprintf("The note is one line of at most %d characters.", domain.MaxKBReviewNote)}
	}
	reviewers := slices.Compact(slices.Sorted(slices.Values(request.ReviewerIDs)))
	if len(reviewers) == 0 || len(reviewers) > domain.MaxKBReviewers {
		return "", nil, &domain.InvalidKBError{Message: fmt.Sprintf("Name 1 to %d reviewers in reviewer_ids: teammates who may publish (GET /api/v1/agents).", domain.MaxKBReviewers)}
	}
	for _, id := range reviewers {
		if !savedReplyID.MatchString(id) {
			return "", nil, &domain.InvalidKBError{Message: "reviewer_ids are agent ids (GET /api/v1/agents)."}
		}
		if id == actor.AgentID {
			return "", nil, &domain.InvalidKBError{Message: "You cannot review your own text: name a teammate who may publish."}
		}
	}
	return note, reviewers, nil
}

// checkKBReviewers refuses a reviewer who is not an active member allowed to publish, or who wrote part
// of the text.
func checkKBReviewers(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, reviewers []string) error {
	rows, err := tx.Query(ctx, `SELECT a.id::text FROM agents a WHERE a.workspace_id=$1 AND a.id=ANY($2::uuid[]) AND a.deactivated_at IS NULL AND `+HoldsPermission("$3"),
		workspaceID, reviewers, kbPublishPermission)
	if err != nil {
		return err
	}
	allowed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range reviewers {
		if !slices.Contains(allowed, id) {
			return &domain.InvalidKBError{Message: "Reviewer " + id + " is not an active teammate who may publish (" + kbPublishPermission + "): only they can approve a text. GET /api/v1/agents lists the team."}
		}
	}
	authors, err := kbTextAuthors(ctx, tx, workspaceID, articleID, locale, reviewers)
	if err != nil {
		return err
	}
	if len(authors) > 0 {
		return &domain.InvalidKBError{Message: "Reviewer " + authors[0] + " changed this text since it was last published, and an author never reviews their own text: name a teammate who did not."}
	}
	return nil
}

// lockKBArticleForReview locks a live article, so its versions are numbered one at a time, and returns
// its status and whether it has a draft.
func lockKBArticleForReview(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) (string, bool, error) {
	var status string
	var hasDraft bool
	err := tx.QueryRow(ctx, `SELECT a.status, EXISTS (SELECT 1 FROM kb_article_drafts d WHERE d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL)
		FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2 AND a.deleted_at IS NULL FOR UPDATE OF a`, workspaceID, articleID).Scan(&status, &hasDraft)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, domain.ErrKBArticleNotFound
	}
	return status, hasDraft, err
}

func insertKBReview(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID string, locale *string, hash, note string, supersedes *string, reviewers []string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO kb_reviews(workspace_id,article_id,locale,content_hash,note,requested_by_agent_id,requested_by_token_id,requested_by_token_label,supersedes_review_id)
		SELECT $1,$2,$3,$4,$5,$6,t.id,t.label,$8 FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$7 RETURNING id::text`,
		workspaceID, articleID, locale, hash, note, optionalID(actor.AgentID), optionalID(actor.TokenID), supersedes).Scan(&id); err != nil {
		return "", fmt.Errorf("insert knowledge base review: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_review_reviewers(workspace_id,review_id,agent_id) SELECT $1,$2,unnest($3::uuid[])`, workspaceID, id, reviewers); err != nil {
		return "", fmt.Errorf("insert knowledge base reviewers: %w", err)
	}
	return id, nil
}

// addKBReviewVersion records a review event in the history of the article (locale nil) or of its
// translation, with the text it is about: the draft over the live article or translation. It
// changes no text, so it leaves the help center and updated_at alone.
func addKBReviewVersion(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, actor Actor, kind string) error {
	if locale != nil {
		status, err := insertKBTranslationVersion(ctx, tx, workspaceID, articleID, *locale, actor, kind, nil, true)
		if err != nil {
			return err
		}
		return noteKBTextWriter(ctx, tx, workspaceID, articleID, locale, actor, kind, status)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,seo_title,seo_description,status,collection_id,
			show_contents,changed_by_agent_id,token_id,token_label,created_at)
		SELECT a.workspace_id,a.id,COALESCE((SELECT max(version) FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2),0)+1,
			$3,COALESCE(d.title,a.title),COALESCE(d.slug,a.slug),COALESCE(d.body_md,a.body_md),COALESCE(d.seo_title,a.seo_title),COALESCE(d.seo_description,a.seo_description),
			a.status,COALESCE(d.collection_id,a.collection_id),COALESCE(d.show_contents,a.show_contents),$4,t.id,t.label,clock_timestamp()
		FROM kb_articles a LEFT JOIN kb_article_drafts d ON d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL
		LEFT JOIN sessions t ON t.workspace_id=a.workspace_id AND t.id=$5
		WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, articleID, kind, optionalID(actor.AgentID), optionalID(actor.TokenID)); err != nil {
		return fmt.Errorf("record knowledge base review version: %w", err)
	}
	return nil
}
