package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Every write here that touches several library rows locks them in id order (FOR UPDATE ... ORDER BY
// id), as linkKBMedia does, so two of them never wait on each other in a cycle. The check that an item
// is in use runs in a statement after the lock: in READ COMMITTED it sees every link a save committed
// while the lock was waited for, and a save that comes later waits for this transaction.

// lockKBMedia locks, in id order, the rows of ids on one side of the library's trash (trashed false:
// live rows, for a move to the trash; true: rows in the trash, for a restore or a purge), and reads
// where every id of the workspace is: true in the trash, false outside it, absent when not found.
// Locking only one side keeps a trash from waiting on a save that is taking an item out of the trash
// (lockNamedKBMedia), which would close a cycle.
func lockKBMedia(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, trashed bool) (map[string]bool, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND (deleted_at IS NOT NULL)=$3 ORDER BY id FOR UPDATE`,
		workspaceID, ids, trashed); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text, deleted_at IS NOT NULL FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	where := map[string]bool{}
	var id string
	var inTrash bool
	_, err = pgx.ForEachRow(rows, []any{&id, &inTrash}, func() error {
		where[id] = inTrash
		return nil
	})
	return where, err
}

// kbMediaIDsInUse returns which of ids a live article's current text names.
func kbMediaIDsInUse(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (map[string]bool, error) {
	return kbMediaIDsWhere(ctx, tx, workspaceID, ids, kbMediaInUse)
}

// kbMediaIDsWhere returns which of ids meet condition, an SQL rule on library rows aliased m.
func kbMediaIDsWhere(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, condition string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT m.id::text FROM kb_media m WHERE m.workspace_id=$1 AND m.id=ANY($2::uuid[]) AND `+condition, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[string])
	inUse := map[string]bool{}
	for _, id := range used {
		inUse[id] = true
	}
	return inUse, err
}

// validKBMediaIDs refuses a list that is empty, too long or holds something that is not an id, and
// returns the ids in lowercase, each once, in the order given.
func validKBMediaIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > domain.MaxKBMediaBatch {
		return nil, &domain.InvalidKBError{Message: fmt.Sprintf("Send 1 to %d ids.", domain.MaxKBMediaBatch)}
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !savedReplyID.MatchString(id) {
			return nil, &domain.InvalidKBError{Message: "Every id is a media id, such as one from the library list."}
		}
		id = strings.ToLower(id)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// addToKBMediaBatch adds ids to the trash batch batchID, or to a new one when batchID is empty, and
// returns its id. A batch of another workspace is not found.
func addToKBMediaBatch(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, batchID string, ids []string) (string, error) {
	if batchID == "" {
		err := tx.QueryRow(ctx, `INSERT INTO kb_media_batches(workspace_id,action,media_ids,created_by_agent_id) VALUES($1,'trash',$2::uuid[],$3) RETURNING id::text`,
			workspaceID, ids, optionalID(actor.AgentID)).Scan(&batchID)
		return batchID, err
	}
	tag, err := tx.Exec(ctx, `UPDATE kb_media_batches SET media_ids=media_ids || $3::uuid[], updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`,
		workspaceID, batchID, ids)
	if err == nil && tag.RowsAffected() == 0 {
		return "", domain.ErrKBMediaBatchNotFound
	}
	return batchID, err
}

// TrashKBMedia moves the items of ids to the library's trash, each one only while no live article's
// current text names it, and answers each id: trashed, already_in_trash, in_use or not_found. The
// items it trashed join batchID (a new batch when empty), whose id it returns, so one Undo restores them
// all; no batch is made when nothing moved.
func (s *Store) TrashKBMedia(ctx context.Context, workspaceID string, actor Actor, ids []string, batchID string) ([]domain.KBMediaResult, string, error) {
	ids, err := validKBMediaIDs(ids)
	if err != nil {
		return nil, "", err
	}
	if batchID != "" && !savedReplyID.MatchString(batchID) {
		return nil, "", domain.ErrKBMediaBatchNotFound
	}
	type answer struct {
		results []domain.KBMediaResult
		batch   string
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (answer, error) {
		trashed, err := lockKBMedia(ctx, tx, workspaceID, ids, false)
		if err != nil {
			return answer{}, err
		}
		inUse, err := kbMediaIDsInUse(ctx, tx, workspaceID, ids)
		if err != nil {
			return answer{}, err
		}
		var moved []string
		results := make([]domain.KBMediaResult, len(ids))
		for i, id := range ids {
			inTrash, found := trashed[id]
			status := domain.KBMediaTrashed
			switch {
			case !found:
				status = domain.KBMediaNotFound
			case inTrash:
				status = domain.KBMediaAlreadyInTrash
			case inUse[id]:
				status = domain.KBMediaInUse
			default:
				moved = append(moved, id)
			}
			results[i] = domain.KBMediaResult{ID: id, Status: status}
		}
		if len(moved) == 0 {
			return answer{results: results, batch: batchID}, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_media SET deleted_at=clock_timestamp(), deleted_by_agent_id=$3, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, moved, optionalID(actor.AgentID)); err != nil {
			return answer{}, err
		}
		batch, err := addToKBMediaBatch(ctx, tx, workspaceID, actor, batchID, moved)
		return answer{results: results, batch: batch}, err
	})
	if err != nil {
		return nil, "", kbError("trash knowledge base media", err)
	}
	return out.results, out.batch, nil
}

// TrashUnusedKBMedia moves to the trash at most domain.MaxKBMediaUnusedBatch items that no live article
// uses and that were added before before (when the person opened the page, so an item uploaded since is
// never swept along), leaving kept items unless includeKept. It returns how many moved, whether more
// are left, and the batch (batchID, or a new one) a single Undo restores.
func (s *Store) TrashUnusedKBMedia(ctx context.Context, workspaceID string, actor Actor, includeKept bool, before time.Time, batchID string) (int, bool, string, error) {
	if batchID != "" && !savedReplyID.MatchString(batchID) {
		return 0, false, "", domain.ErrKBMediaBatchNotFound
	}
	type answer struct {
		moved int
		more  bool
		batch string
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (answer, error) {
		rows, err := tx.Query(ctx, `SELECT m.id::text FROM kb_media m WHERE m.workspace_id=$1 AND m.deleted_at IS NULL AND m.created_at<$2
				AND ($3 OR NOT m.kept) AND NOT `+kbMediaInUse+` AND NOT `+kbMediaNamedByHelpCenter+` ORDER BY m.id LIMIT `+strconv.Itoa(domain.MaxKBMediaUnusedBatch)+` FOR UPDATE OF m SKIP LOCKED`,
			workspaceID, before, includeKept)
		if err != nil {
			return answer{}, err
		}
		picked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(picked) == 0 {
			return answer{batch: batchID}, err
		}
		inUse, err := kbMediaIDsInUse(ctx, tx, workspaceID, picked)
		if err != nil {
			return answer{}, err
		}
		var moved []string
		for _, id := range picked {
			if !inUse[id] {
				moved = append(moved, id)
			}
		}
		more := len(picked) == domain.MaxKBMediaUnusedBatch
		if len(moved) == 0 {
			return answer{more: more, batch: batchID}, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_media SET deleted_at=clock_timestamp(), deleted_by_agent_id=$3, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, moved, optionalID(actor.AgentID)); err != nil {
			return answer{}, err
		}
		batch, err := addToKBMediaBatch(ctx, tx, workspaceID, actor, batchID, moved)
		return answer{moved: len(moved), more: more, batch: batch}, err
	})
	if err != nil {
		return 0, false, "", kbError("trash unused knowledge base media", err)
	}
	return out.moved, out.more, out.batch, nil
}

// UnusedKBMediaIDs lists up to limit items TrashUnusedKBMedia would move, for an agent request that
// names them.
func (s *Store) UnusedKBMediaIDs(ctx context.Context, workspaceID string, includeKept bool, before time.Time, limit int) ([]string, error) {
	ids, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT m.id::text FROM kb_media m WHERE m.workspace_id=$1 AND m.deleted_at IS NULL AND m.created_at<$2
			AND ($3 OR NOT m.kept) AND NOT `+kbMediaInUse+` AND NOT `+kbMediaNamedByHelpCenter+` ORDER BY m.size DESC, m.id LIMIT $4`, workspaceID, before, includeKept, limit)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
	if err != nil {
		return nil, fmt.Errorf("list unused knowledge base media: %w", err)
	}
	return ids, nil
}

// RestoreKBMedia takes items out of the library's trash, those of ids or every item of batchID, and
// answers each: restored, not_in_trash or not_found.
func (s *Store) RestoreKBMedia(ctx context.Context, workspaceID string, ids []string, batchID string) ([]domain.KBMediaResult, error) {
	if batchID == "" {
		valid, err := validKBMediaIDs(ids)
		if err != nil {
			return nil, err
		}
		ids = valid
	} else if !savedReplyID.MatchString(batchID) || len(ids) > 0 {
		if len(ids) > 0 {
			return nil, &domain.InvalidKBError{Message: "Send ids or batch_id, not both."}
		}
		return nil, domain.ErrKBMediaBatchNotFound
	}
	results, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBMediaResult, error) {
		if batchID != "" {
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT array_agg(DISTINCT member::text ORDER BY member::text) FROM unnest(b.media_ids) member), '{}')
				FROM kb_media_batches b WHERE b.workspace_id=$1 AND b.id=$2`, workspaceID, batchID).Scan(&ids); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, domain.ErrKBMediaBatchNotFound
				}
				return nil, err
			}
		}
		trashed, err := lockKBMedia(ctx, tx, workspaceID, ids, true)
		if err != nil {
			return nil, err
		}
		var back []string
		results := make([]domain.KBMediaResult, len(ids))
		for i, id := range ids {
			inTrash, found := trashed[id]
			status := domain.KBMediaRestored
			switch {
			case !found:
				status = domain.KBMediaNotFound
			case !inTrash:
				status = domain.KBMediaNotInTrash
			default:
				back = append(back, id)
			}
			results[i] = domain.KBMediaResult{ID: id, Status: status}
		}
		// The 30 days an item nobody kept and no article uses has before the purge start again.
		_, err = tx.Exec(ctx, `UPDATE kb_media SET deleted_at=NULL, deleted_by_agent_id=NULL, updated_at=clock_timestamp(), last_uploaded_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, back)
		return results, err
	})
	if err != nil {
		return nil, kbError("restore knowledge base media", err)
	}
	return results, nil
}

// PurgeKBMediaNow deletes for good the items of ids that are in the library's trash, with their files,
// and answers each: purged, not_in_trash, not_found, or in_use for the help center's logo or favicon,
// which stays in the trash until the theme names another image. Articles in the trash and older
// versions that named one then show "Media no longer exists".
func (s *Store) PurgeKBMediaNow(ctx context.Context, workspaceID string, ids []string) ([]domain.KBMediaResult, error) {
	ids, err := validKBMediaIDs(ids)
	if err != nil {
		return nil, err
	}
	type answer struct {
		results []domain.KBMediaResult
		keys    []string
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (answer, error) {
		trashed, err := lockKBMedia(ctx, tx, workspaceID, ids, true)
		if err != nil {
			return answer{}, err
		}
		inUse, err := kbMediaIDsInUse(ctx, tx, workspaceID, ids)
		if err != nil {
			return answer{}, err
		}
		named, err := kbMediaIDsWhere(ctx, tx, workspaceID, ids, kbMediaNamedByHelpCenter)
		if err != nil {
			return answer{}, err
		}
		var gone []string
		results := make([]domain.KBMediaResult, len(ids))
		for i, id := range ids {
			inTrash, found := trashed[id]
			status := domain.KBMediaPurged
			switch {
			case !found:
				status = domain.KBMediaNotFound
			case !inTrash || inUse[id]:
				status = domain.KBMediaNotInTrash
			case named[id]:
				// The help center's logo or favicon waits in the trash until the theme lets go of it.
				status = domain.KBMediaInUse
			default:
				gone = append(gone, id)
			}
			results[i] = domain.KBMediaResult{ID: id, Status: status}
		}
		rows, err := tx.Query(ctx, `DELETE FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[])
			RETURNING storage_key, COALESCE(thumb_storage_key,''), COALESCE(poster_storage_key,'')`, workspaceID, gone)
		if err != nil {
			return answer{}, err
		}
		var keys []string
		var key, thumb, poster string
		_, err = pgx.ForEachRow(rows, []any{&key, &thumb, &poster}, func() error {
			for _, stored := range []string{key, thumb, poster} {
				if stored != "" {
					keys = append(keys, stored)
				}
			}
			return nil
		})
		return answer{results: results, keys: keys}, err
	})
	if err != nil {
		return nil, kbError("purge knowledge base media", err)
	}
	if s.objects != nil {
		for _, key := range out.keys {
			if err := s.DiscardObject(ctx, workspaceID, key); err != nil {
				return out.results, fmt.Errorf("delete purged knowledge base media file: %w", err)
			}
		}
	}
	return out.results, nil
}

// ListTrashedKBMedia pages through the library's trash, last moved there first, with each item's usage
// and when the purge deletes it: none for an item the purge keeps.
func (s *Store) ListTrashedKBMedia(ctx context.Context, workspaceID, cursor string, limit int) ([]domain.KBMedia, string, error) {
	if limit < 1 || limit > domain.MaxKBMediaPage {
		limit = 60
	}
	var after kbMediaCursor
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || !savedReplyID.MatchString(after.ID) {
			return nil, "", &domain.InvalidKBError{Message: "The cursor is not one this list gave."}
		}
		if _, err := time.Parse(time.RFC3339Nano, after.Key); err != nil {
			return nil, "", &domain.InvalidKBError{Message: "The cursor is not one this list gave."}
		}
	}
	condition := `AND $2::text IS NULL AND $3::text IS NULL`
	var key, id any
	if cursor != "" {
		condition, key, id = `AND (m.deleted_at, m.id) < ($2::timestamptz, $3::uuid)`, after.Key, after.ID
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]StoredKBMedia, error) {
		rows, err := tx.Query(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.deleted_at IS NOT NULL `+condition+`
			ORDER BY m.deleted_at DESC, m.id DESC LIMIT `+strconv.Itoa(limit+1), workspaceID, key, id)
		if err != nil {
			return nil, err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (StoredKBMedia, error) { return scanKBMedia(row) })
		if err != nil {
			return nil, err
		}
		return items, addKBMediaUsage(ctx, tx, workspaceID, items)
	})
	if err != nil {
		return nil, "", fmt.Errorf("list knowledge base media trash: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(kbMediaCursor{Key: last.DeletedAt.Format(time.RFC3339Nano), ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	out := make([]domain.KBMedia, len(items))
	for i, item := range items {
		out[i] = item.KBMedia
		// The purge keeps an item a live article or the help center still names (kbMediaPurgeable).
		if usage := item.Usage; usage == nil || (usage.Articles == 0 && len(usage.HelpCenter) == 0) {
			purgeAt := item.DeletedAt.Add(domain.KBTrashRetention)
			out[i].PurgeAt = &purgeAt
		}
	}
	return out, next, nil
}

// KBMediaInUseArticles lists the live articles whose current text names the item, for a refusal to
// trash it.
func (s *Store) KBMediaInUseArticles(ctx context.Context, workspaceID, id string) ([]domain.KBMediaArticle, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBMediaArticle, error) {
		rows, err := tx.Query(ctx, `SELECT a.id::text,a.number,a.title,a.status,false,true
			FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND a.deleted_at IS NULL ORDER BY a.number`, workspaceID, id)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBMediaArticle, error) {
			var article domain.KBMediaArticle
			return article, row.Scan(&article.ID, &article.Number, &article.Title, &article.Status, &article.InTrash, &article.Current)
		})
	})
}

// MissingKBMedia returns which of the media ids a text names the workspace does not have, for the
// warning a save answers with ("Media no longer exists").
func (s *Store) MissingKBMedia(ctx context.Context, workspaceID string, ids []string) ([]string, error) {
	missing := []string{}
	if len(ids) == 0 {
		return missing, nil
	}
	found, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT id::text FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
	if err != nil {
		return nil, fmt.Errorf("check knowledge base media: %w", err)
	}
	have := map[string]bool{}
	for _, id := range found {
		have[id] = true
	}
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// purgeKBMediaBatches forgets trash batches older than the trash keeps anything, in every workspace,
// inside PurgeKBTrash's privileged transaction.
func purgeKBMediaBatches(ctx context.Context, tx pgx.Tx, cutoff time.Time) error {
	_, err := tx.Exec(ctx, `DELETE FROM kb_media_batches WHERE updated_at<$1`, cutoff)
	return err
}
