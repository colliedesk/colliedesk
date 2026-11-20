package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbTextVersion is the SQL condition that version row v holds the text the article had then. A draft
// save and a review event hold text that was not published, so they never count as the article's text.
func kbTextVersion(v string) string {
	return v + `kind NOT IN ('` + domain.KBArticleDrafted + `','` + domain.KBReviewRequested + `','` + domain.KBReviewApprovedKind + `','` +
		domain.KBReviewChangesKind + `','` + domain.KBReviewWithdrawnKind + `')`
}

// kbApprovalRequired tells whether a text listed in any of collections needs an approved version
// before it is published: a collection set to on, or set to inherit while the workspace requires it.
func kbApprovalRequired(ctx context.Context, tx pgx.Tx, workspaceID string, collections []string) (bool, error) {
	var required bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_collections c LEFT JOIN help_centers h ON h.workspace_id=c.workspace_id
		WHERE c.workspace_id=$1 AND c.id=ANY($2::uuid[]) AND (c.require_approval='on' OR (c.require_approval='inherit' AND COALESCE(h.require_approval,false))))`,
		workspaceID, collections).Scan(&required)
	if err != nil {
		return false, fmt.Errorf("read knowledge base approval setting: %w", err)
	}
	return required, nil
}

// checkKBApproval returns domain.ErrKBApprovalRequired unless a review approved exactly the text with hash.
func checkKBApproval(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, hash string) error {
	var approved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_reviews WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3
		AND state='approved' AND content_hash=$4)`, workspaceID, articleID, locale, hash).Scan(&approved); err != nil {
		return fmt.Errorf("read knowledge base approval: %w", err)
	}
	if !approved && locale != nil {
		return domain.ErrKBTranslationApprovalRequired
	}
	if !approved {
		return domain.ErrKBApprovalRequired
	}
	return nil
}

// guardKBPublish refuses a write that puts text on the help center no review approved, where any
// collection the text was or is listed in needs approval. It runs after the write, in its transaction,
// so every way a text gets published passes it: a change of status, a published draft, a restore, a
// new article created published. before is the text as it was (zero for a new one); a published text
// that stays the same, or a text that leaves the help center, passes.
func guardKBPublish(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, before, after kbPublishState) error {
	if !after.Live {
		return nil
	}
	hash := after.Content.Hash()
	if before.Live && before.Content.Hash() == hash {
		return nil
	}
	required, err := kbApprovalRequired(ctx, tx, workspaceID, slices.Concat(before.Content.CollectionIDs, after.Content.CollectionIDs))
	if err != nil || !required {
		return err
	}
	return checkKBApproval(ctx, tx, workspaceID, articleID, locale, hash)
}

// kbPublishState is what guardKBPublish compares: whether the text is on the help center, and what it is.
type kbPublishState struct {
	Live    bool
	Content domain.KBContent
}

func articlePublishState(article domain.KBArticle) kbPublishState {
	return kbPublishState{Live: article.ID != "" && article.Status == domain.KBPublished && article.DeletedAt == nil, Content: article.Content()}
}
