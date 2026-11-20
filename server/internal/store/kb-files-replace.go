package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// swapKBFileBytes trades the bytes of two locked files (key, hash, size, type, name and text): the row
// of each id then holds what the other held, so every article and address of either keeps its id.
// A hash is unique in a workspace and checked row by row, so a first holds a hash of its own id while
// b takes a's bytes.
func swapKBFileBytes(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, a, b string) error {
	type fileBytes struct {
		key, sha, contentType, name string
		size                        int64
		text                        *string
	}
	read := func(id string) (fileBytes, error) {
		var f fileBytes
		err := tx.QueryRow(ctx, `SELECT storage_key,sha256,content_type,name,size,text FROM kb_media WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id).Scan(&f.key, &f.sha, &f.contentType, &f.name, &f.size, &f.text)
		return f, err
	}
	write := func(id string, f fileBytes) error {
		_, err := tx.Exec(ctx, `UPDATE kb_media SET storage_key=$3, sha256=$4, content_type=$5, name=$6, size=$7, text=$8,
			updated_at=clock_timestamp(), updated_by_agent_id=$9 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, f.key, f.sha, f.contentType, f.name, f.size, f.text, optionalID(actor.AgentID))
		return err
	}
	first, err := read(a)
	if err != nil {
		return err
	}
	second, err := read(b)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_media SET sha256=md5(id::text)||md5(id::text) WHERE workspace_id=$1 AND id=$2`, workspaceID, a); err != nil {
		return err
	}
	if err := write(b, first); err != nil {
		return err
	}
	return write(a, second)
}

// replaceKBFileInPlace puts the bytes of the file to into the file from, which keeps its id and so its
// address and every list it is on, and moves from's old bytes into to, which goes to the trash as the
// earlier version. to must be on no article's list: it would show the old bytes from then on.
func replaceKBFileInPlace(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, from, to string) (domain.KBMediaReplacement, error) {
	if err := swapKBFileBytes(ctx, tx, workspaceID, actor, from, to); err != nil {
		return domain.KBMediaReplacement{}, err
	}
	// Read after the swap locked both rows, so a list saved meanwhile has committed and is seen.
	var listed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_article_attachments WHERE workspace_id=$1 AND media_id=$2)
		OR EXISTS(SELECT 1 FROM kb_article_drafts WHERE workspace_id=$1 AND $2::uuid=ANY(attachment_ids))`, workspaceID, to).Scan(&listed); err != nil {
		return domain.KBMediaReplacement{}, err
	}
	if listed {
		return domain.KBMediaReplacement{}, &domain.InvalidKBError{Message: "An article lists the file to put in its place; upload the new version as a file no article lists."}
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_media SET deleted_at=clock_timestamp(), deleted_by_agent_id=$3 WHERE workspace_id=$1 AND id=$2`,
		workspaceID, to, optionalID(actor.AgentID)); err != nil {
		return domain.KBMediaReplacement{}, err
	}
	return scanKBMediaReplacement(tx.QueryRow(ctx, `INSERT INTO kb_media_replacements(workspace_id,from_media_id,to_media_id,article_ids,article_versions,translations,replaced_by_agent_id,in_place)
		VALUES($1,$2,$3,'{}','{}','[]',$4,true) RETURNING `+kbMediaReplacementColumns, workspaceID, from, to, optionalID(actor.AgentID)))
}

// undoKBFileReplacement moves the bytes back: from gets its earlier version again and to, taken out of
// the trash by the lock, its own upload. An older replacement of the same file waits for the newer
// one to be undone first, so each undo moves the bytes it moved.
func undoKBFileReplacement(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, replacement domain.KBMediaReplacement) (domain.KBMediaReplacement, error) {
	var newer bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_media_replacements WHERE workspace_id=$1 AND in_place AND undone_at IS NULL
		AND (from_media_id=$2 OR to_media_id=$2 OR from_media_id=$3 OR to_media_id=$3) AND created_at>$4)`,
		workspaceID, replacement.From, replacement.To, replacement.CreatedAt).Scan(&newer); err != nil {
		return domain.KBMediaReplacement{}, err
	}
	if newer {
		return domain.KBMediaReplacement{}, &domain.InvalidKBError{Message: "This file was replaced again since; undo the newer replacement first."}
	}
	found, err := lockNamedKBMedia(ctx, tx, workspaceID, []string{replacement.From, replacement.To})
	if err != nil {
		return domain.KBMediaReplacement{}, err
	}
	if len(found) != 2 {
		return domain.KBMediaReplacement{}, domain.ErrKBMediaGone
	}
	if err := swapKBFileBytes(ctx, tx, workspaceID, actor, replacement.From, replacement.To); err != nil {
		return domain.KBMediaReplacement{}, err
	}
	return scanKBMediaReplacement(tx.QueryRow(ctx, `UPDATE kb_media_replacements SET undone_at=clock_timestamp(), undone_by_agent_id=$3
		WHERE workspace_id=$1 AND id=$2 RETURNING `+kbMediaReplacementColumns, workspaceID, replacement.ID, optionalID(actor.AgentID)))
}
