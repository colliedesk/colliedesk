package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbTextWriteKinds are the writes in which someone wrote the text: its writer, for the four-eyes
// rule. A draft is one; a review event, a deletion or a move to and from the trash is not.
var kbTextWriteKinds = []string{domain.KBArticleCreated, domain.KBArticleEdited, domain.KBArticleRestored,
	domain.KBArticleDrafted, domain.KBArticleImported, domain.KBArticleMediaReplaced}

// kbHelpCenterTextKind tells whether a version of kind that has status published puts the text on
// the help center, as kbTextVersion does: a draft or a review event does not.
func kbHelpCenterTextKind(kind string) bool {
	return !slices.Contains([]string{domain.KBArticleDrafted, domain.KBReviewRequested, domain.KBReviewApprovedKind,
		domain.KBReviewChangesKind, domain.KBReviewWithdrawnKind}, kind)
}

// noteKBTextWriter runs after every write of the text of an article (locale nil) or of its
// translation, a version or an autosave, of kind with status the text has after it. A write that puts
// the text on the help center forgets who wrote it; any other write of the text adds the person who
// made it to its writers, the authors the four-eyes rule keeps from reviewing it.
func noteKBTextWriter(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, actor Actor, kind, status string) error {
	if status == domain.KBPublished && kbHelpCenterTextKind(kind) {
		if _, err := tx.Exec(ctx, `DELETE FROM kb_text_writers WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3`,
			workspaceID, articleID, locale); err != nil {
			return fmt.Errorf("clear knowledge base text writers: %w", err)
		}
		return nil
	}
	if actor.AgentID == "" || !slices.Contains(kbTextWriteKinds, kind) {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_text_writers(workspace_id,article_id,locale,agent_id) VALUES ($1,$2,$3,$4)
		ON CONFLICT ON CONSTRAINT kb_text_writers_key DO NOTHING`, workspaceID, articleID, locale, actor.AgentID); err != nil {
		return fmt.Errorf("record knowledge base text writer: %w", err)
	}
	return nil
}
