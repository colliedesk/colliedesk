package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/jackc/pgx/v5"
)

// CachedMailImage points at the stored bytes of a remote image the proxy fetched for a workspace.
type CachedMailImage struct {
	StorageKey  string
	ContentType string
	Size        int64
	FetchedAt   time.Time
}

// GetCachedMailImage returns the cache row of a URL hash in the workspace, if any.
func (s *Store) GetCachedMailImage(ctx context.Context, workspaceID, urlHash string) (CachedMailImage, bool, error) {
	var image CachedMailImage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT storage_key,content_type,size,fetched_at FROM mail_image_cache WHERE workspace_id=$1 AND url_hash=$2`,
			workspaceID, urlHash).Scan(&image.StorageKey, &image.ContentType, &image.Size, &image.FetchedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CachedMailImage{}, false, nil
	}
	if err != nil {
		return CachedMailImage{}, false, fmt.Errorf("read mail image cache: %w", err)
	}
	return image, true, nil
}

// SaveCachedMailImage records the bytes already written to storage for a URL hash, replacing an older
// row. The object it replaced is deleted when nothing else points at it.
func (s *Store) SaveCachedMailImage(ctx context.Context, workspaceID, urlHash, contentType string, object attachments.Object) error {
	var previous *string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := VerifyObjects(ctx, tx, s.objects, object.Key); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT storage_key FROM mail_image_cache WHERE workspace_id=$1 AND url_hash=$2 FOR UPDATE`, workspaceID, urlHash).Scan(&previous); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read previous mail image: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mail_image_cache(workspace_id,url_hash,storage_key,content_type,size,fetched_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())
			ON CONFLICT (workspace_id,url_hash) DO UPDATE SET storage_key=EXCLUDED.storage_key,content_type=EXCLUDED.content_type,size=EXCLUDED.size,fetched_at=EXCLUDED.fetched_at`,
			workspaceID, urlHash, object.Key, contentType, object.Size); err != nil {
			return fmt.Errorf("save mail image cache: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if previous != nil && *previous != object.Key {
		return s.DiscardObject(ctx, workspaceID, *previous)
	}
	return nil
}
