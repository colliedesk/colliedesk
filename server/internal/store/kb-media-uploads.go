package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

const kbMediaUploadColumns = `id::text,kind,name,size,received,expires_at,media_id::text`

func scanKBMediaUpload(row pgx.Row) (domain.KBMediaUpload, error) {
	var upload domain.KBMediaUpload
	err := row.Scan(&upload.ID, &upload.Kind, &upload.Name, &upload.Size, &upload.Offset, &upload.ExpiresAt, &upload.MediaID)
	upload.ExpiresAt = domain.UTC(upload.ExpiresAt)
	upload.PartSize = domain.KBMediaPartSize
	return upload, err
}

// KBMediaUploadReserved is the room the workspace's uploads in progress still need: what they
// announced and have not received.
func kbMediaUploadReserved(ctx context.Context, tx pgx.Tx, workspaceID string) (int64, error) {
	var reserved int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(sum(size-received),0) FROM kb_media_uploads
		WHERE workspace_id=$1 AND completed_at IS NULL AND expires_at>clock_timestamp()`, workspaceID).Scan(&reserved)
	return reserved, err
}

// CreateKBMediaUpload starts an upload in parts of a video of input.Size bytes. It refuses, before any
// byte is sent, a video the plan's storage has no room for counting the uploads in progress, a
// workspace with too many uploads in progress or a full library, and a person who started too many in
// the last hour. It returns the room the workspace's other uploads still reserve, for the disk check.
func (s *Store) CreateKBMediaUpload(ctx context.Context, workspaceID string, actor Actor, input domain.NewKBMediaUpload) (domain.KBMediaUpload, int64, error) {
	upload, err := cleanKBImageUpload(input.KBImageUpload)
	if err != nil {
		return domain.KBMediaUpload{}, 0, err
	}
	type answer struct {
		upload   domain.KBMediaUpload
		reserved int64
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (answer, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return answer{}, err
		}
		var open, lastHour, media int
		if err := tx.QueryRow(ctx, `SELECT
				count(*) FILTER (WHERE completed_at IS NULL AND expires_at>clock_timestamp()),
				count(*) FILTER (WHERE created_at>clock_timestamp()-interval '1 hour' AND created_by_agent_id IS NOT DISTINCT FROM $2::uuid),
				(SELECT count(*) FROM kb_media WHERE workspace_id=$1)
			FROM kb_media_uploads WHERE workspace_id=$1`, workspaceID, optionalID(actor.AgentID)).Scan(&open, &lastHour, &media); err != nil {
			return answer{}, err
		}
		if open >= domain.MaxKBMediaUploadsOpen || lastHour >= domain.MaxKBMediaUploadsPerHour {
			return answer{}, domain.ErrKBMediaUploadLimit
		}
		if media+open >= domain.MaxKBMedia {
			return answer{}, domain.ErrKBMediaLimit
		}
		reserved, err := kbMediaUploadReserved(ctx, tx, workspaceID)
		if err != nil {
			return answer{}, err
		}
		if err := s.CheckStorage(ctx, tx, workspaceID, input.Size+reserved, s.now()); err != nil {
			return answer{}, err
		}
		created, err := scanKBMediaUpload(tx.QueryRow(ctx, `INSERT INTO kb_media_uploads(workspace_id,name,alt,keep,size,created_by_agent_id,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+$7::interval) RETURNING `+kbMediaUploadColumns,
			workspaceID, upload.Name, upload.Alt, upload.Keep, input.Size, optionalID(actor.AgentID), domain.KBMediaUploadTTL.String()))
		return answer{upload: created, reserved: reserved}, err
	})
	if err != nil {
		return domain.KBMediaUpload{}, 0, kbError("start knowledge base media upload", err)
	}
	return out.upload, out.reserved, nil
}

// GetKBMediaUpload returns an upload of the workspace that has not expired.
func (s *Store) GetKBMediaUpload(ctx context.Context, workspaceID, id string) (domain.KBMediaUpload, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBMediaUpload{}, domain.ErrKBMediaUploadNotFound
	}
	upload, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBMediaUpload, error) {
		return scanKBMediaUpload(tx.QueryRow(ctx, `SELECT `+kbMediaUploadColumns+` FROM kb_media_uploads
			WHERE workspace_id=$1 AND id=$2 AND expires_at>clock_timestamp()`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBMediaUpload{}, domain.ErrKBMediaUploadNotFound
	}
	if err != nil {
		return domain.KBMediaUpload{}, fmt.Errorf("get knowledge base media upload: %w", err)
	}
	return upload, nil
}

// RecordKBMediaUploadPart moves an upload from offset from to offset to once the part is written.
// Another part that got there first answers a *domain.KBMediaOffsetError with where it stands.
func (s *Store) RecordKBMediaUploadPart(ctx context.Context, workspaceID, id string, from, to int64) (domain.KBMediaUpload, error) {
	upload, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBMediaUpload, error) {
		upload, err := scanKBMediaUpload(tx.QueryRow(ctx, `UPDATE kb_media_uploads SET received=$4, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND received=$3 AND completed_at IS NULL AND expires_at>clock_timestamp() AND $4<=size
			RETURNING `+kbMediaUploadColumns, workspaceID, id, from, to))
		if errors.Is(err, pgx.ErrNoRows) {
			current, err := scanKBMediaUpload(tx.QueryRow(ctx, `SELECT `+kbMediaUploadColumns+` FROM kb_media_uploads
				WHERE workspace_id=$1 AND id=$2 AND expires_at>clock_timestamp()`, workspaceID, id))
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.KBMediaUpload{}, domain.ErrKBMediaUploadNotFound
			}
			if err != nil {
				return domain.KBMediaUpload{}, err
			}
			return current, &domain.KBMediaOffsetError{Offset: current.Offset}
		}
		return upload, err
	})
	var offset *domain.KBMediaOffsetError
	if err != nil && !errors.As(err, &offset) && !errors.Is(err, domain.ErrKBMediaUploadNotFound) {
		return domain.KBMediaUpload{}, fmt.Errorf("record knowledge base media upload part: %w", err)
	}
	return upload, err
}

// CancelKBMediaUpload forgets an upload in progress; the caller removes its file.
func (s *Store) CancelKBMediaUpload(ctx context.Context, workspaceID, id string) error {
	if !savedReplyID.MatchString(id) {
		return domain.ErrKBMediaUploadNotFound
	}
	gone, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(ctx, `DELETE FROM kb_media_uploads WHERE workspace_id=$1 AND id=$2 AND completed_at IS NULL`, workspaceID, id)
		return tag.RowsAffected() == 1, err
	})
	if err != nil {
		return fmt.Errorf("cancel knowledge base media upload: %w", err)
	}
	if !gone {
		return domain.ErrKBMediaUploadNotFound
	}
	return nil
}

// CheckKBVideoAdmission refuses, before a completed upload's file is moved into storage, a video the
// library or the plan has no room for; CompleteKBMediaUpload checks again.
func (s *Store) CheckKBVideoAdmission(ctx context.Context, workspaceID string, size int64) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
			return struct{}{}, err
		}
		if count >= domain.MaxKBMedia {
			return struct{}{}, domain.ErrKBMediaLimit
		}
		return struct{}{}, s.CheckStorage(ctx, tx, workspaceID, size, s.now())
	})
	return kbError("check knowledge base video", err)
}

// NewKBVideo is a completed upload's video: its stored bytes and what the server read from them.
type NewKBVideo struct {
	Object      attachments.Object
	ContentType string
	Width       int
	Height      int
	DurationMS  *int64
	Codecs      []string
}

// CompleteKBMediaUpload records the video of an upload whose every byte arrived, checked and stored
// in object, with the name and alt text the upload gave; the same bytes give the item that holds them,
// and created false. The plan's room is checked again here, where it is authoritative. When it fails,
// the caller owns the stored bytes.
func (s *Store) CompleteKBMediaUpload(ctx context.Context, workspaceID string, actor Actor, id string, video NewKBVideo) (domain.KBMedia, bool, error) {
	created := false
	media, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBMedia, error) {
		var name, alt string
		var keep bool
		var size, received int64
		var done *string
		err := tx.QueryRow(ctx, `SELECT name,alt,keep,size,received,media_id::text FROM kb_media_uploads
			WHERE workspace_id=$1 AND id=$2 AND expires_at>clock_timestamp() FOR UPDATE`, workspaceID, id).Scan(&name, &alt, &keep, &size, &received, &done)
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredKBMedia{}, domain.ErrKBMediaUploadNotFound
		}
		if err != nil {
			return StoredKBMedia{}, err
		}
		if done != nil {
			return scanKBMedia(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, *done))
		}
		if received != size || video.Object.Size != size {
			return StoredKBMedia{}, domain.ErrKBMediaUploadIncomplete
		}
		upload := domain.KBImageUpload{Name: name, Alt: alt, Keep: keep}
		media, err := touchKBImage(ctx, tx, workspaceID, video.Object.SHA256, upload)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := lockKB(ctx, tx, workspaceID); err != nil {
				return StoredKBMedia{}, err
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
				return StoredKBMedia{}, err
			}
			if count >= domain.MaxKBMedia {
				return StoredKBMedia{}, domain.ErrKBMediaLimit
			}
			if err := s.CheckStorage(ctx, tx, workspaceID, video.Object.Size, s.now()); err != nil {
				return StoredKBMedia{}, err
			}
			if err := VerifyObjects(ctx, tx, s.objects, video.Object.Key); err != nil {
				return StoredKBMedia{}, err
			}
			codecs := video.Codecs
			if codecs == nil {
				codecs = []string{}
			}
			media, err = scanKBMedia(tx.QueryRow(ctx, `INSERT INTO kb_media AS m(workspace_id,kind,storage_key,sha256,size,content_type,created_by_agent_id,
					name,alt,width,height,duration_ms,codecs,inspected_at,kept,updated_by_agent_id)
				VALUES($1,'video',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,clock_timestamp(),$13,$6) ON CONFLICT (workspace_id, sha256) DO NOTHING RETURNING `+kbMediaColumns,
				workspaceID, video.Object.Key, video.Object.SHA256, video.Object.Size, video.ContentType, optionalID(actor.AgentID),
				name, alt, video.Width, video.Height, video.DurationMS, codecs, keep))
			if errors.Is(err, pgx.ErrNoRows) {
				// Another upload of the same bytes completed first; answer its item.
				media, err = touchKBImage(ctx, tx, workspaceID, video.Object.SHA256, upload)
			} else {
				created = err == nil
			}
		}
		if err != nil {
			return StoredKBMedia{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE kb_media_uploads SET completed_at=clock_timestamp(), media_id=$3, received=size WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, media.ID)
		return media, err
	})
	if err != nil {
		return domain.KBMedia{}, false, kbError("complete knowledge base media upload", err)
	}
	return media.KBMedia, created, nil
}

// expireKBMediaUploads deletes, in every workspace, the uploads past their time inside PurgeKBTrash's
// privileged transaction and returns their ids, whose staged files the caller removes, with the ids of
// staged that no upload in progress names any more (a part written while its upload was cancelled).
func expireKBMediaUploads(ctx context.Context, tx pgx.Tx, now time.Time, staged []string) ([]string, error) {
	rows, err := tx.Query(ctx, `DELETE FROM kb_media_uploads WHERE expires_at<$1 RETURNING id::text`, now)
	if err != nil {
		return nil, err
	}
	expired, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(staged) == 0 {
		return expired, err
	}
	rows, err = tx.Query(ctx, `SELECT s.id FROM unnest($1::text[]) s(id)
		WHERE NOT EXISTS (SELECT 1 FROM kb_media_uploads u WHERE u.id::text=s.id AND u.completed_at IS NULL)`, staged)
	if err != nil {
		return nil, err
	}
	orphans, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return append(expired, orphans...), err
}

// SetKBVideoPoster gives a video of the library its poster (stored image bytes, metadata removed) and
// the poster's thumbnail, which the library shows for the video; the poster counts toward the plan
// like the video. It returns the storage keys of the poster and thumbnail it replaced, for the caller
// to discard. Images have no poster.
func (s *Store) SetKBVideoPoster(ctx context.Context, workspaceID string, actor Actor, id string, poster attachments.Object, contentType string, thumb *attachments.Object) (domain.KBMedia, []string, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBMedia{}, nil, domain.ErrKBMediaNotFound
	}
	type answer struct {
		media StoredKBMedia
		old   []string
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (answer, error) {
		var kind, oldPoster, oldThumb string
		var oldPosterSize int64
		err := tx.QueryRow(ctx, `SELECT kind, COALESCE(poster_storage_key,''), COALESCE(poster_size,0), COALESCE(thumb_storage_key,'') FROM kb_media
			WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&kind, &oldPoster, &oldPosterSize, &oldThumb)
		if errors.Is(err, pgx.ErrNoRows) {
			return answer{}, domain.ErrKBMediaNotFound
		}
		if err != nil {
			return answer{}, err
		}
		if kind != domain.KBMediaVideo {
			return answer{}, &domain.InvalidKBError{Message: "Only a video has a poster."}
		}
		if err := s.CheckStorage(ctx, tx, workspaceID, max(0, poster.Size-oldPosterSize), s.now()); err != nil {
			return answer{}, err
		}
		keys := []string{poster.Key}
		var thumbKey *string
		var thumbSize *int64
		if thumb != nil {
			keys, thumbKey, thumbSize = append(keys, thumb.Key), &thumb.Key, &thumb.Size
		}
		if err := VerifyObjects(ctx, tx, s.objects, keys...); err != nil {
			return answer{}, err
		}
		media, err := scanKBMedia(tx.QueryRow(ctx, `UPDATE kb_media m SET poster_storage_key=$3, poster_size=$4, poster_content_type=$8, thumb_storage_key=$5, thumb_size=$6,
				updated_at=clock_timestamp(), updated_by_agent_id=$7
			WHERE m.workspace_id=$1 AND m.id=$2 RETURNING `+kbMediaColumns, workspaceID, id, poster.Key, poster.Size, thumbKey, thumbSize, optionalID(actor.AgentID), contentType))
		if err != nil {
			return answer{}, err
		}
		var old []string
		for _, key := range []string{oldPoster, oldThumb} {
			if key != "" && key != poster.Key && (thumb == nil || key != thumb.Key) {
				old = append(old, key)
			}
		}
		// A published article shows the poster; its page names the new one.
		return answer{media: media, old: old}, bumpKB(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.KBMedia{}, nil, kbError("set knowledge base video poster", err)
	}
	return out.media.KBMedia, out.old, nil
}
