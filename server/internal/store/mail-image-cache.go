package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
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

// CachedMailImageBytes is how many bytes of images the workspace has cached.
func (s *Store) CachedMailImageBytes(ctx context.Context, workspaceID string) (int64, error) {
	var total int64
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(sum(size),0)::bigint FROM mail_image_cache WHERE workspace_id=$1`, workspaceID).Scan(&total)
	})
	if err != nil {
		return 0, fmt.Errorf("measure mail image cache: %w", err)
	}
	return total, nil
}

// TouchCachedMailImage records that the image was just shown. It writes at most once a day per image, so
// viewing a long thread does not turn into a write per image per view.
func (s *Store) TouchCachedMailImage(ctx context.Context, workspaceID, urlHash string) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE mail_image_cache SET last_served_at=now() WHERE workspace_id=$1 AND url_hash=$2 AND last_served_at < now() - interval '1 day'`, workspaceID, urlHash)
		return err
	})
}

type evictedImage struct{ workspaceID, key string }

// SweepMailImageCache keeps the image cache bounded, in every workspace: it deletes the images fetched
// more than maxAge ago, then, while a workspace's rest is over maxBytes (the same cap that stops it caching
// more), that workspace's images that were served least recently. A workspace over its share loses its own
// images and never another's. The total is therefore at most the number of workspaces times maxBytes.
// It returns how many cache rows it removed. The stored bytes go with them unless an attachment still
// uses the same file.
func (s *Store) SweepMailImageCache(ctx context.Context, maxAge time.Duration, maxBytes int64) (int, error) {
	var evicted []evictedImage
	err := s.db.Privileged(ctx, database.PrivilegeMailImageSweep, func(tx pgx.Tx) error {
		collect := func(sql string, args ...any) error {
			rows, err := tx.Query(ctx, sql, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var item evictedImage
				if err := rows.Scan(&item.workspaceID, &item.key); err != nil {
					return err
				}
				evicted = append(evicted, item)
			}
			return rows.Err()
		}
		if err := collect(`DELETE FROM mail_image_cache WHERE fetched_at < $1 RETURNING workspace_id::text, storage_key`, s.now().Add(-maxAge)); err != nil {
			return fmt.Errorf("evict old mail images: %w", err)
		}
		// Newest-served first within each workspace: what fits under its cap stays, every row past it goes.
		if err := collect(`WITH ranked AS (
				SELECT workspace_id, url_hash, sum(size) OVER (PARTITION BY workspace_id ORDER BY last_served_at DESC, fetched_at DESC, url_hash) AS running
				FROM mail_image_cache)
			DELETE FROM mail_image_cache c USING ranked r
			WHERE c.workspace_id=r.workspace_id AND c.url_hash=r.url_hash AND r.running > $1
			RETURNING c.workspace_id::text, c.storage_key`, maxBytes); err != nil {
			return fmt.Errorf("evict mail images over the cap: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if s.objects == nil {
		return len(evicted), nil
	}
	seen := map[evictedImage]bool{}
	for _, item := range evicted {
		if seen[item] {
			continue
		}
		seen[item] = true
		if err := s.DiscardObject(ctx, item.workspaceID, item.key); err != nil {
			return len(evicted), fmt.Errorf("delete evicted mail image: %w", err)
		}
	}
	return len(evicted), nil
}
