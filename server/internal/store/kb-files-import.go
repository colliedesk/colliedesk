package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ImportKBFile adds a file an import stored at object to the media library in tx, or answers the
// item that already holds the same bytes, taken out of the trash; created tells which. text is the
// file's extracted text ("" for none). Like an imported image it is not counted towards storage.
func ImportKBFile(ctx context.Context, tx pgx.Tx, storage attachments.Storage, workspaceID string, object attachments.Object, contentType, name, text string) (string, bool, error) {
	upload, err := cleanKBImageUpload(domain.KBImageUpload{Name: name})
	if err != nil {
		return "", false, err
	}
	existing, err := touchKBImage(ctx, tx, workspaceID, object.SHA256, upload)
	if err == nil {
		if existing.Kind != domain.KBMediaFile {
			return "", false, fmt.Errorf("the bytes of %s are already an %s of the media library", name, existing.Kind)
		}
		return existing.ID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if err := VerifyObjects(ctx, tx, storage, object.Key); err != nil {
		return "", false, err
	}
	var stored any
	if text != "" {
		stored = text
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO kb_media(workspace_id,kind,storage_key,sha256,size,content_type,counted,name,text,inspected_at)
		VALUES($1,$2,$3,$4,$5,$6,false,$7,$8,clock_timestamp()) RETURNING id::text`,
		workspaceID, domain.KBMediaFile, object.Key, object.SHA256, object.Size, contentType, upload.Name, stored).Scan(&id); err != nil {
		return "", false, fmt.Errorf("import knowledge base file: %w", err)
	}
	return id, true, nil
}

// ImportKBAttachments makes ids, files of the library, the live list of an imported article (locale
// nil) or of one of its translations, with the checks a person's list gets.
func ImportKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, ids []string) error {
	if err := checkKBAttachments(ctx, tx, workspaceID, ids); err != nil {
		return err
	}
	return replaceKBAttachments(ctx, tx, workspaceID, articleID, locale, ids)
}
