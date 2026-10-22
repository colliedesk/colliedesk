package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// An import points the links of what it wrote at their new addresses only while nobody changed that
// content in Colliedesk; anything changed since is left as it is. Both functions below write in the
// importer's transaction and report whether they wrote.

// RewriteImportedKBArticleBody replaces the text of an imported article with body when its text is
// still was, it is outside the trash and its newest version is the import's, as a new version of kind
// import (undo and later imports treat it as the import's, and the earlier text stays restorable). The
// row is locked before the check. The article keeps its timestamps: they are the source's.
func RewriteImportedKBArticleBody(ctx context.Context, tx pgx.Tx, workspaceID, id, was, body string) (bool, error) {
	if err := domain.CheckKBBody(body); err != nil {
		return false, err
	}
	var current string
	var live bool
	err := tx.QueryRow(ctx, `SELECT body_md, deleted_at IS NULL FROM kb_articles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&current, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || !live || current != was {
		return false, err
	}
	if _, imported, err := KBArticleImportedLast(ctx, tx, workspaceID, id); err != nil || !imported {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET body_md=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, id, body); err != nil {
		return false, fmt.Errorf("rewrite imported knowledge base article: %w", err)
	}
	return true, addKBArticleVersion(ctx, tx, workspaceID, id, Actor{}, domain.KBArticleImported, nil)
}

// RewriteImportedSavedReplyBody replaces the text of an imported saved reply with body when its text
// is still was, it is outside the trash and nobody saved it since it was created (updated_at equals
// created_at), with a version of the kind the import writes (create, no person). updated_at does not
// move, so undo still removes the reply and a later import may still rewrite it.
func RewriteImportedSavedReplyBody(ctx context.Context, tx pgx.Tx, workspaceID, id, was, body string) (bool, error) {
	if err := domain.CheckSavedReplyBody(body); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE saved_replies SET body=$4 WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND updated_at=created_at AND body=$3`,
		workspaceID, id, was, body)
	if err != nil {
		return false, fmt.Errorf("rewrite imported saved reply: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	_, err = addSavedReplyVersion(ctx, tx, workspaceID, id, Actor{}, domain.SavedReplyCreated, nil)
	return err == nil, err
}
