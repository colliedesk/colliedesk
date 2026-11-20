package freshdesk

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// kbFileUnlisted holds for a library item k that no article, translation or draft lists.
const kbFileUnlisted = `NOT EXISTS (SELECT 1 FROM kb_article_attachments l WHERE l.workspace_id=k.workspace_id AND l.media_id=k.id)
	AND NOT EXISTS (SELECT 1 FROM kb_article_drafts d WHERE d.workspace_id=k.workspace_id AND k.id=ANY(d.attachment_ids))`

// kbFilePersonsWork holds for a library item k a person kept, or replaced in place (its bytes are then theirs).
const kbFilePersonsWork = `(k.kept OR EXISTS (SELECT 1 FROM kb_media_replacements r WHERE r.workspace_id=k.workspace_id AND r.from_media_id=k.id AND r.in_place))`

// undoKBFiles removes the files the import created that nothing lists once its articles are gone, and
// answers their storage keys. A file a person kept or replaced stays and is counted.
func undoKBFiles(ctx context.Context, tx pgx.Tx, workspaceID, source string, result *UndoResult) ([]string, error) {
	const imported = `i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_file' AND (i.meta->>'created')::boolean`
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media k JOIN import_items i ON i.workspace_id=k.workspace_id AND i.target_id=k.id
		WHERE `+imported+` AND `+kbFilePersonsWork+` AND `+kbFileUnlisted, workspaceID, source).Scan(&result.KeptKBFiles); err != nil {
		return nil, fmt.Errorf("count kept knowledge base files: %w", err)
	}
	rows, err := tx.Query(ctx, `DELETE FROM kb_media k USING import_items i WHERE `+imported+`
		AND k.workspace_id=i.workspace_id AND k.id=i.target_id AND NOT `+kbFilePersonsWork+` AND `+kbFileUnlisted+`
		RETURNING k.storage_key`, workspaceID, source)
	if err != nil {
		return nil, fmt.Errorf("undo knowledge base files: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("undo knowledge base files: %w", err)
	}
	result.KBFiles = len(keys)
	return keys, nil
}
