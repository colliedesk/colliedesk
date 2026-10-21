package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/jackc/pgx/v5"
)

// StoredKBMedia is a library row with the storage keys of its bytes, thumbnail and poster.
type StoredKBMedia struct {
	domain.KBMedia
	StorageKey, ThumbKey, PosterKey string
	LastUploadedAt                  time.Time
}

// StoredKBImage is an image row with the storage key its bytes are under.
type StoredKBImage = StoredKBMedia

const kbMediaColumns = `m.id::text,m.kind,m.storage_key,m.content_type,m.size,m.sha256,m.name,m.alt,m.width,m.height,m.duration_ms,m.codecs,
	COALESCE(m.thumb_storage_key,''),COALESCE(m.poster_storage_key,''),m.kept,m.deleted_at,m.created_at,m.updated_at,m.last_uploaded_at`

func scanKBMedia(row pgx.Row, extra ...any) (StoredKBMedia, error) {
	var media StoredKBMedia
	err := row.Scan(append([]any{&media.ID, &media.Kind, &media.StorageKey, &media.ContentType, &media.Size, &media.SHA256, &media.Name, &media.Alt,
		&media.Width, &media.Height, &media.DurationMS, &media.Codecs, &media.ThumbKey, &media.PosterKey, &media.Kept, &media.DeletedAt,
		&media.CreatedAt, &media.UpdatedAt, &media.LastUploadedAt}, extra...)...)
	media.CreatedAt, media.UpdatedAt, media.LastUploadedAt = domain.UTC(media.CreatedAt), domain.UTC(media.UpdatedAt), domain.UTC(media.LastUploadedAt)
	media.DeletedAt = domain.UTCOptional(media.DeletedAt)
	if media.Codecs == nil {
		media.Codecs = []string{}
	}
	media.URL = domain.KBMediaPath + media.ID
	if media.Kind == domain.KBMediaImage {
		media.URL = domain.KBImagePath + media.ID
	}
	if media.ThumbKey != "" {
		// The key ends in the thumbnail's SHA-256: a new thumbnail (a video's new poster) is a new URL,
		// so the immutable cache of the old one never shows it.
		thumbnail := domain.KBMediaPath + media.ID + "/thumbnail?v=" + media.ThumbKey[max(0, len(media.ThumbKey)-8):]
		media.ThumbnailURL = &thumbnail
	}
	return media, err
}

func scanKBImage(row pgx.Row) (StoredKBImage, error) { return scanKBMedia(row) }

// CheckKBImageAdmission refuses early, before the bytes are written, an image the plan's storage cap
// has no room for. The check inside CreateKBImage stays authoritative.
func (s *Store) CheckKBImageAdmission(ctx context.Context, workspaceID string, size int64) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, s.CheckStorage(ctx, tx, workspaceID, size, s.now())
	})
	return err
}

// touchKBImage returns the item of the workspace that holds the bytes with sha256 and marks it
// uploaded now, so the purge of unused images counts its 30 days from this upload. Uploading bytes
// that are in the trash takes them out of it; an upload made in the library keeps them; a name or alt
// text the upload gives fills one the item does not have yet, and never replaces one.
func touchKBImage(ctx context.Context, tx pgx.Tx, workspaceID, sha256 string, upload domain.KBImageUpload) (StoredKBImage, error) {
	return scanKBImage(tx.QueryRow(ctx, `UPDATE kb_media m SET last_uploaded_at=clock_timestamp(), kept=m.kept OR $3,
		name=CASE WHEN m.name='' THEN $4 ELSE m.name END, alt=CASE WHEN m.alt='' THEN $5 ELSE m.alt END,
		deleted_at=NULL, deleted_by_agent_id=NULL WHERE m.workspace_id=$1 AND m.sha256=$2 RETURNING `+kbMediaColumns,
		workspaceID, sha256, upload.Keep, upload.Name, upload.Alt))
}

// cleanKBImageUpload trims the name and alt text an upload gives and checks their length.
func cleanKBImageUpload(upload domain.KBImageUpload) (domain.KBImageUpload, error) {
	var err error
	if upload.Name, err = domain.CleanKBMediaText("name", upload.Name, domain.MaxKBMediaName); err != nil {
		return upload, err
	}
	upload.Alt, err = domain.CleanKBMediaText("alt text", upload.Alt, domain.MaxKBMediaAlt)
	return upload, err
}

// FindKBImage returns the item that already holds the bytes with sha256, marked uploaded now, and
// false when there is none. An upload of bytes the workspace has needs no storage room.
func (s *Store) FindKBImage(ctx context.Context, workspaceID, sha256 string, upload domain.KBImageUpload) (domain.KBMedia, bool, error) {
	upload, err := cleanKBImageUpload(upload)
	if err != nil {
		return domain.KBMedia{}, false, err
	}
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) { return touchKBImage(ctx, tx, workspaceID, sha256, upload) })
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBMedia{}, false, nil
	}
	if err != nil {
		return domain.KBMedia{}, false, fmt.Errorf("find knowledge base image: %w", err)
	}
	return image.KBMedia, true, nil
}

// NewKBImage is an uploaded image: its stored bytes, what the server read from them, and the
// thumbnail when one was made.
type NewKBImage struct {
	Object        attachments.Object
	ContentType   string
	Width, Height int
	Thumb         *attachments.Object
	domain.KBImageUpload
}

// CreateKBImage records stored image bytes for the workspace. The same bytes give the image that
// already holds them, and created false; the new thumbnail is then the caller's to discard. When it
// fails, the caller owns the stored bytes and thumbnail.
func (s *Store) CreateKBImage(ctx context.Context, workspaceID string, actor Actor, input NewKBImage) (domain.KBMedia, bool, error) {
	upload, err := cleanKBImageUpload(input.KBImageUpload)
	if err != nil {
		return domain.KBMedia{}, false, err
	}
	name, alt := upload.Name, upload.Alt
	created := false
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) {
		existing, err := touchKBImage(ctx, tx, workspaceID, input.Object.SHA256, upload)
		if err == nil || !errors.Is(err, pgx.ErrNoRows) {
			return existing, err
		}
		var thumbKey *string
		var thumbSize *int64
		keys := []string{input.Object.Key}
		if input.Thumb != nil {
			thumbKey, thumbSize = &input.Thumb.Key, &input.Thumb.Size
			keys = append(keys, input.Thumb.Key)
		}
		if err := s.CheckStorage(ctx, tx, workspaceID, input.Object.Size, s.now()); err != nil {
			return StoredKBImage{}, err
		}
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return StoredKBImage{}, err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
			return StoredKBImage{}, err
		}
		if count >= domain.MaxKBMedia {
			return StoredKBImage{}, domain.ErrKBMediaLimit
		}
		if err := VerifyObjects(ctx, tx, s.objects, keys...); err != nil {
			return StoredKBImage{}, err
		}
		image, err := scanKBImage(tx.QueryRow(ctx, `INSERT INTO kb_media AS m(workspace_id,storage_key,sha256,size,content_type,created_by_agent_id,
				name,alt,width,height,thumb_storage_key,thumb_size,inspected_at,kept,updated_by_agent_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,clock_timestamp(),$13,$6) ON CONFLICT (workspace_id, sha256) DO NOTHING RETURNING `+kbMediaColumns,
			workspaceID, input.Object.Key, input.Object.SHA256, input.Object.Size, input.ContentType, optionalID(actor.AgentID),
			name, alt, input.Width, input.Height, thumbKey, thumbSize, input.Keep))
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent upload of the same bytes won; answer its image.
			return touchKBImage(ctx, tx, workspaceID, input.Object.SHA256, upload)
		}
		created = err == nil
		return image, err
	})
	if err != nil {
		return domain.KBMedia{}, false, fmt.Errorf("store knowledge base image: %w", err)
	}
	return image.KBMedia, created, nil
}

// GetKBImage returns an image of the workspace with its storage key.
func (s *Store) GetKBImage(ctx context.Context, workspaceID, id string) (StoredKBImage, error) {
	image, err := s.GetKBMedia(ctx, workspaceID, id)
	if errors.Is(err, domain.ErrKBMediaNotFound) || err == nil && image.Kind != domain.KBMediaImage {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	return image, err
}

// GetKBMedia returns a library item of the workspace, in the trash or not, with its storage keys.
func (s *Store) GetKBMedia(ctx context.Context, workspaceID, id string) (StoredKBMedia, error) {
	if !savedReplyID.MatchString(id) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	media, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBMedia, error) {
		return scanKBMedia(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	if err != nil {
		return StoredKBMedia{}, fmt.Errorf("get knowledge base media: %w", err)
	}
	return media, nil
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

// kbMediaPurgeable is the purge rule for library items, on rows aliased m: in the trash for longer than
// domain.KBTrashRetention, or never kept, linked by no article (no version either) and last uploaded
// before that. Either way no live article's current text may name it.
const kbMediaPurgeable = `((m.deleted_at IS NOT NULL AND m.deleted_at<$1)
		OR (m.deleted_at IS NULL AND NOT m.kept AND m.last_uploaded_at<$1
			AND NOT EXISTS(SELECT 1 FROM kb_article_media l WHERE l.workspace_id=m.workspace_id AND l.media_id=m.id)))
	AND NOT EXISTS(SELECT 1 FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		WHERE l.workspace_id=m.workspace_id AND l.media_id=m.id AND l.current AND a.deleted_at IS NULL)`

// PurgeKBTrash deletes for good, in every workspace, the articles and collections that have been in
// the trash longer than domain.KBTrashRetention (with the articles' history, translations and media
// links), then the library items kbMediaPurgeable names. Only the storage keys leave the privileged
// transaction; their bytes go through the scoped DiscardObject, which keeps bytes another row still
// uses. serve runs it at start and daily.
//
// A save that links an item locks its row FOR SHARE (linkKBMedia). The purge locks the rows it will
// delete first, skipping locked ones, and only then checks the rule again in a new statement, which
// sees every link committed before its lock; a later save waits for it and finds the item gone.
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
		rows, err := tx.Query(ctx, `SELECT m.id::text FROM kb_media m WHERE `+kbMediaPurgeable+` FOR UPDATE OF m SKIP LOCKED`, cutoff)
		if err != nil {
			return fmt.Errorf("lock purged knowledge base media: %w", err)
		}
		locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(locked) == 0 {
			return err
		}
		rows, err = tx.Query(ctx, `DELETE FROM kb_media m WHERE m.id=ANY($2::uuid[]) AND `+kbMediaPurgeable+`
			RETURNING m.workspace_id::text, m.storage_key, COALESCE(m.thumb_storage_key,''), COALESCE(m.poster_storage_key,'')`, cutoff, locked)
		if err != nil {
			return fmt.Errorf("purge knowledge base media: %w", err)
		}
		var workspaceID, key, thumb, poster string
		_, err = pgx.ForEachRow(rows, []any{&workspaceID, &key, &thumb, &poster}, func() error {
			purged.Images++
			for _, stored := range []string{key, thumb, poster} {
				if stored != "" {
					orphans = append(orphans, orphan{workspaceID, stored})
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("purge knowledge base media: %w", err)
		}
		return nil
	})
	if err != nil {
		return KBPurge{}, err
	}
	if s.objects != nil {
		for _, item := range orphans {
			if err := s.DiscardObject(ctx, item.workspaceID, item.key); err != nil {
				return purged, fmt.Errorf("delete purged knowledge base media file: %w", err)
			}
		}
	}
	return purged, nil
}

// KBBackfillBatch is how many older images one backfill round reads.
const KBBackfillBatch = 50

// BackfillKBMedia reads the size in pixels and makes the thumbnail of library images stored before
// the server did it at upload (images from before the media library, and imported ones, whose bytes
// an import keeps as Freshdesk served them), at most limit of them. Only the ids leave the privileged
// transaction; each image is read and updated in its own workspace. An image that cannot be read is
// marked inspected all the same, so it is tried once. It returns how many it took. serve runs it with
// the purge until none is left.
func (s *Store) BackfillKBMedia(ctx context.Context, limit int) (int, error) {
	if s.objects == nil {
		return 0, nil
	}
	type pending struct{ workspaceID, id, key string }
	var todo []pending
	err := s.db.Privileged(ctx, database.PrivilegeKBPurge, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,id::text,storage_key FROM kb_media
			WHERE kind='image' AND inspected_at IS NULL ORDER BY created_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		todo, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
			var item pending
			return item, row.Scan(&item.workspaceID, &item.id, &item.key)
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("find knowledge base images to backfill: %w", err)
	}
	for _, item := range todo {
		if err := s.backfillKBImage(ctx, item.workspaceID, item.id, item.key); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

func (s *Store) backfillKBImage(ctx context.Context, workspaceID, id, key string) error {
	var width, height *int
	var thumb *attachments.Object
	if data, err := s.readKBObject(ctx, key); err == nil {
		if info, err := media.InspectImage(data); err == nil {
			width, height = &info.Width, &info.Height
			if thumbnail, _, err := media.Thumbnail(ctx, info, data); err == nil {
				if object, err := s.objects.Put(ctx, workspaceID, bytes.NewReader(thumbnail), int64(len(thumbnail))); err == nil {
					thumb = &object
				} else {
					return fmt.Errorf("store knowledge base thumbnail: %w", err)
				}
			} else if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	var thumbKey *string
	var thumbSize *int64
	if thumb != nil {
		thumbKey, thumbSize = &thumb.Key, &thumb.Size
	}
	updated, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		if thumb != nil {
			if err := VerifyObjects(ctx, tx, s.objects, thumb.Key); err != nil {
				return false, err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE kb_media SET width=$3, height=$4, thumb_storage_key=$5, thumb_size=$6, inspected_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND inspected_at IS NULL`, workspaceID, id, width, height, thumbKey, thumbSize)
		return tag.RowsAffected() == 1, err
	})
	if err != nil {
		return fmt.Errorf("backfill knowledge base image: %w", err)
	}
	if !updated && thumb != nil {
		return s.DiscardObject(ctx, workspaceID, thumb.Key)
	}
	return nil
}

// readKBObject reads stored bytes of at most domain.MaxKBImageBytes.
func (s *Store) readKBObject(ctx context.Context, key string) ([]byte, error) {
	body, err := s.objects.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, domain.MaxKBImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > domain.MaxKBImageBytes {
		return nil, errors.New("stored image is larger than an image may be")
	}
	return data, nil
}
