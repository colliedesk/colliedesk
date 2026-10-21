package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// StoredKBImage is an image row with the storage key its bytes are under.
type StoredKBImage struct {
	domain.KBImage
	StorageKey string
}

func scanKBImage(row pgx.Row) (StoredKBImage, error) {
	var image StoredKBImage
	err := row.Scan(&image.ID, &image.StorageKey, &image.ContentType, &image.Size, &image.SHA256, &image.CreatedAt)
	image.CreatedAt = domain.UTC(image.CreatedAt)
	image.URL = domain.KBImagePath + image.ID
	return image, err
}

const kbImageColumns = `id::text,storage_key,content_type,size,sha256,created_at`

// CheckKBImageAdmission refuses early, before the bytes are written, an image the plan's storage cap
// has no room for. The check inside CreateKBImage stays authoritative.
func (s *Store) CheckKBImageAdmission(ctx context.Context, workspaceID string, size int64) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, s.CheckStorage(ctx, tx, workspaceID, size, s.now())
	})
	return err
}

// touchKBImage returns the image of the workspace that holds the bytes with sha256 and marks it
// uploaded now, so the purge of unused images counts its 30 days from this upload.
func touchKBImage(ctx context.Context, tx pgx.Tx, workspaceID, sha256 string) (StoredKBImage, error) {
	return scanKBImage(tx.QueryRow(ctx, `UPDATE kb_images SET last_uploaded_at=clock_timestamp() WHERE workspace_id=$1 AND sha256=$2 RETURNING `+kbImageColumns,
		workspaceID, sha256))
}

// FindKBImage returns the image that already holds the bytes with sha256, marked uploaded now, and
// false when there is none. An upload of bytes the workspace has needs no storage room.
func (s *Store) FindKBImage(ctx context.Context, workspaceID, sha256 string) (domain.KBImage, bool, error) {
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) { return touchKBImage(ctx, tx, workspaceID, sha256) })
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBImage{}, false, nil
	}
	if err != nil {
		return domain.KBImage{}, false, fmt.Errorf("find knowledge base image: %w", err)
	}
	return image.KBImage, true, nil
}

// CreateKBImage records stored image bytes for the workspace. The same bytes give the image that
// already holds them. When it fails, the caller owns the stored bytes.
func (s *Store) CreateKBImage(ctx context.Context, workspaceID string, actor Actor, object attachments.Object, contentType string) (domain.KBImage, error) {
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) {
		existing, err := touchKBImage(ctx, tx, workspaceID, object.SHA256)
		if err == nil || !errors.Is(err, pgx.ErrNoRows) {
			return existing, err
		}
		if err := s.CheckStorage(ctx, tx, workspaceID, object.Size, s.now()); err != nil {
			return StoredKBImage{}, err
		}
		if err := VerifyObjects(ctx, tx, s.objects, object.Key); err != nil {
			return StoredKBImage{}, err
		}
		image, err := scanKBImage(tx.QueryRow(ctx, `INSERT INTO kb_images(workspace_id,storage_key,sha256,size,content_type,created_by_agent_id)
			VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (workspace_id, sha256) DO NOTHING RETURNING `+kbImageColumns,
			workspaceID, object.Key, object.SHA256, object.Size, contentType, optionalID(actor.AgentID)))
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent upload of the same bytes won; answer its image.
			return touchKBImage(ctx, tx, workspaceID, object.SHA256)
		}
		return image, err
	})
	if err != nil {
		return domain.KBImage{}, fmt.Errorf("store knowledge base image: %w", err)
	}
	return image.KBImage, nil
}

// GetKBImage returns an image of the workspace with its storage key.
func (s *Store) GetKBImage(ctx context.Context, workspaceID, id string) (StoredKBImage, error) {
	if !savedReplyID.MatchString(id) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) {
		return scanKBImage(tx.QueryRow(ctx, `SELECT `+kbImageColumns+` FROM kb_images WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	if err != nil {
		return StoredKBImage{}, fmt.Errorf("get knowledge base image: %w", err)
	}
	return image, nil
}

// GetHelpCenter returns the help center settings; a workspace without a row has it off.
func (s *Store) GetHelpCenter(ctx context.Context, workspaceID string) (domain.HelpCenter, error) {
	center, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenter, error) { return readHelpCenter(ctx, tx, workspaceID) })
	if err != nil {
		return domain.HelpCenter{}, fmt.Errorf("get help center: %w", err)
	}
	return center, nil
}

func readHelpCenter(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.HelpCenter, error) {
	center := domain.HelpCenter{Locale: "en"}
	err := tx.QueryRow(ctx, `SELECT enabled,title,locale,content_version,updated_at FROM help_centers WHERE workspace_id=$1`, workspaceID).
		Scan(&center.Enabled, &center.Title, &center.Locale, &center.ContentVersion, &center.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return center, nil
	}
	center.UpdatedAt = domain.UTCOptional(center.UpdatedAt)
	return center, err
}

// PutHelpCenter turns the help center on or off and sets its title and language; it bumps the
// content version, since every page shows them.
func (s *Store) PutHelpCenter(ctx context.Context, workspaceID string, enabled bool, title, locale string) (domain.HelpCenter, error) {
	if err := domain.CheckLocale(locale); err != nil {
		return domain.HelpCenter{}, err
	}
	if title != "" {
		cleaned, err := domain.CleanKBText("title", title, domain.MaxHelpCenterTitle)
		if err != nil {
			return domain.HelpCenter{}, err
		}
		title = cleaned
	}
	center, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenter, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO help_centers(workspace_id,enabled,title,locale,content_version,updated_at) VALUES($1,$2,$3,$4,1,clock_timestamp())
			ON CONFLICT (workspace_id) DO UPDATE SET enabled=$2, title=$3, locale=$4, content_version=help_centers.content_version+1, updated_at=clock_timestamp()`,
			workspaceID, enabled, title, locale); err != nil {
			return domain.HelpCenter{}, err
		}
		return readHelpCenter(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.HelpCenter{}, fmt.Errorf("put help center: %w", err)
	}
	return center, nil
}

// KBPurge counts what PurgeKBTrash deleted.
type KBPurge struct {
	Articles, Collections, Images int64
}

// PurgeKBTrash deletes for good, in every workspace, the articles and collections that have been in
// the trash longer than domain.KBTrashRetention (with the articles' history, translations and image
// links), then the images no article links that were last uploaded before that. Only the image keys leave the
// privileged transaction; their bytes go through the scoped DiscardObject, which keeps bytes another
// row still uses. serve runs it at start and daily.
func (s *Store) PurgeKBTrash(ctx context.Context) (KBPurge, error) {
	var purged KBPurge
	type orphan struct{ workspaceID, key string }
	var orphans []orphan
	cutoff := s.now().Add(-domain.KBTrashRetention)
	err := s.db.Privileged(ctx, database.PrivilegeKBPurge, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM kb_articles WHERE deleted_at IS NOT NULL AND deleted_at<$1`, cutoff)
		if err != nil {
			return fmt.Errorf("purge knowledge base articles: %w", err)
		}
		purged.Articles = tag.RowsAffected()
		// A trashed parent goes once its trashed children went, so repeat once per level.
		for range domain.MaxKBDepth {
			tag, err := tx.Exec(ctx, `DELETE FROM kb_collections c WHERE c.deleted_at IS NOT NULL AND c.deleted_at<$1
				AND NOT EXISTS(SELECT 1 FROM kb_articles a WHERE a.workspace_id=c.workspace_id AND a.collection_id=c.id)
				AND NOT EXISTS(SELECT 1 FROM kb_collections k WHERE k.workspace_id=c.workspace_id AND k.parent_id=c.id)`, cutoff)
			if err != nil {
				return fmt.Errorf("purge knowledge base collections: %w", err)
			}
			purged.Collections += tag.RowsAffected()
		}
		rows, err := tx.Query(ctx, `DELETE FROM kb_images i WHERE i.last_uploaded_at<$1
			AND NOT EXISTS(SELECT 1 FROM kb_article_images l WHERE l.workspace_id=i.workspace_id AND l.image_id=i.id)
			RETURNING i.workspace_id::text, i.storage_key`, cutoff)
		if err != nil {
			return fmt.Errorf("purge knowledge base images: %w", err)
		}
		found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (orphan, error) {
			var item orphan
			return item, row.Scan(&item.workspaceID, &item.key)
		})
		orphans, purged.Images = found, int64(len(found))
		return err
	})
	if err != nil {
		return KBPurge{}, err
	}
	if s.objects != nil {
		for _, item := range orphans {
			if err := s.DiscardObject(ctx, item.workspaceID, item.key); err != nil {
				return purged, fmt.Errorf("delete purged knowledge base image file: %w", err)
			}
		}
	}
	return purged, nil
}
