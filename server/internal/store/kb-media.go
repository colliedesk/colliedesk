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

// kbMediaInUse is true for an item, aliased m, that the current text of a live article (or one of its
// translations) names: the library's "in use".
const kbMediaInUse = `EXISTS(SELECT 1 FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
	WHERE l.workspace_id=m.workspace_id AND l.media_id=m.id AND l.current AND a.deleted_at IS NULL)`

// kbMediaCursor is where a page of the library ends: the sort key of its last item and its id.
type kbMediaCursor struct {
	Key string `json:"k"`
	ID  string `json:"id"`
}

// kbMediaOrders are the sorts of the library: the ORDER BY, the keyset condition after a cursor
// ($2 the key, $3 the id) and the key of an item.
var kbMediaOrders = map[string]struct {
	order, after string
	key          func(StoredKBMedia) string
}{
	"newest":  {`m.created_at DESC, m.id DESC`, `(m.created_at, m.id) < ($2::timestamptz, $3::uuid)`, func(m StoredKBMedia) string { return m.CreatedAt.Format(time.RFC3339Nano) }},
	"oldest":  {`m.created_at, m.id`, `(m.created_at, m.id) > ($2::timestamptz, $3::uuid)`, func(m StoredKBMedia) string { return m.CreatedAt.Format(time.RFC3339Nano) }},
	"name":    {`m.name, m.id`, `(m.name, m.id) > ($2::text, $3::uuid)`, func(m StoredKBMedia) string { return m.Name }},
	"largest": {`m.size DESC, m.id DESC`, `(m.size, m.id) < ($2::bigint, $3::uuid)`, func(m StoredKBMedia) string { return strconv.FormatInt(m.Size, 10) }},
}

// ListKBMedia pages through the library outside its trash, newest first unless the filter sorts
// otherwise, with each item's usage. Summary is set on the first page of usage=unused: it adds up
// every unused item the filter selects, not only the page.
func (s *Store) ListKBMedia(ctx context.Context, workspaceID string, filter domain.KBMediaFilter) ([]domain.KBMedia, string, *domain.KBMediaSummary, error) {
	if err := domain.CheckKBMediaFilter(filter); err != nil {
		return nil, "", nil, err
	}
	if filter.Sort == "" {
		filter.Sort = "newest"
	}
	order := kbMediaOrders[filter.Sort]
	if filter.Limit < 1 || filter.Limit > domain.MaxKBMediaPage {
		filter.Limit = 60
	}
	var cursor kbMediaCursor
	if filter.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || !savedReplyID.MatchString(cursor.ID) {
			return nil, "", nil, &domain.InvalidKBError{Message: "The cursor is not one this list gave."}
		}
	}
	// $1 workspace, $2 and $3 the cursor, $4 kind, $5 the search pattern.
	where := `m.workspace_id=$1 AND m.deleted_at IS NULL AND ($4='' OR m.kind=$4)
		AND ($5='' OR m.name ILIKE '%'||$5||'%' ESCAPE '\' OR m.alt ILIKE '%'||$5||'%' ESCAPE '\')`
	switch filter.Usage {
	case "in_use":
		where += ` AND ` + kbMediaInUse
	case "unused":
		where += ` AND NOT ` + kbMediaInUse
	case "missing_alt":
		where += ` AND m.alt=''`
	}
	page := where
	if filter.Cursor != "" {
		page += ` AND ` + order.after
	} else {
		page += ` AND $2::text IS NULL AND $3::text IS NULL`
	}
	var key, id any
	if filter.Cursor != "" {
		key, id = cursor.Key, cursor.ID
	}
	pattern := escapeLikePattern(strings.TrimSpace(filter.Query))
	type listed struct {
		items   []StoredKBMedia
		summary *domain.KBMediaSummary
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (listed, error) {
		// The ids first, from the library index alone where the filter allows, then the rows of the page.
		// The unused page adds up every unused item in the same scan, with window sums taken before LIMIT.
		withSummary := filter.Usage == "unused" && filter.Cursor == ""
		totals := `0::bigint AS count, 0::bigint AS bytes, 0::bigint AS kept_count, 0::bigint AS kept_bytes`
		if withSummary {
			totals = `count(*) FILTER (WHERE NOT m.kept) OVER () AS count,
				COALESCE(sum(m.size+COALESCE(m.poster_size,0)) FILTER (WHERE NOT m.kept) OVER (),0) AS bytes,
				count(*) FILTER (WHERE m.kept) OVER () AS kept_count,
				COALESCE(sum(m.size+COALESCE(m.poster_size,0)) FILTER (WHERE m.kept) OVER (),0) AS kept_bytes`
		}
		rows, err := tx.Query(ctx, `SELECT `+kbMediaColumns+`,p.count,p.bytes,p.kept_count,p.kept_bytes FROM kb_media m
			JOIN (SELECT m.id, `+totals+` FROM kb_media m WHERE `+page+` ORDER BY `+order.order+` LIMIT `+strconv.Itoa(filter.Limit+1)+`) p ON p.id=m.id
			WHERE m.workspace_id=$1 ORDER BY `+order.order, workspaceID, key, id, filter.Kind, pattern)
		if err != nil {
			return listed{}, err
		}
		var summary domain.KBMediaSummary
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (StoredKBMedia, error) {
			return scanKBMedia(row, &summary.Count, &summary.Bytes, &summary.KeptCount, &summary.KeptBytes)
		})
		if err != nil {
			return listed{}, err
		}
		out := listed{items: items}
		if withSummary {
			out.summary = &summary
		}
		return out, addKBMediaUsage(ctx, tx, workspaceID, out.items)
	})
	if err != nil {
		return nil, "", nil, fmt.Errorf("list knowledge base media: %w", err)
	}
	items, next := result.items, ""
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(kbMediaCursor{Key: order.key(last), ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	out := make([]domain.KBMedia, len(items))
	for i, item := range items {
		out[i] = item.KBMedia
	}
	return out, next, result.summary, nil
}

// addKBMediaUsage counts the articles that name each item and sets when the purge deletes it: an item
// nobody kept and no article (no version either) links goes 30 days after its last upload.
func addKBMediaUsage(ctx context.Context, tx pgx.Tx, workspaceID string, items []StoredKBMedia) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	rows, err := tx.Query(ctx, `SELECT l.media_id::text,
			count(*) FILTER (WHERE l.current AND a.deleted_at IS NULL),
			count(*) FILTER (WHERE l.current AND a.deleted_at IS NOT NULL),
			count(*) FILTER (WHERE NOT l.current)
		FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		WHERE l.workspace_id=$1 AND l.media_id=ANY($2::uuid[]) GROUP BY l.media_id`, workspaceID, ids)
	if err != nil {
		return err
	}
	usage := map[string]domain.KBMediaUsage{}
	var id string
	var counts domain.KBMediaUsage
	if _, err := pgx.ForEachRow(rows, []any{&id, &counts.Articles, &counts.TrashedArticles, &counts.OlderVersions}, func() error {
		usage[id] = counts
		return nil
	}); err != nil {
		return err
	}
	for i := range items {
		counts := usage[items[i].ID]
		items[i].Usage = &counts
		if !items[i].Kept && items[i].DeletedAt == nil && counts == (domain.KBMediaUsage{}) {
			purgeAt := items[i].LastUploadedAt.Add(domain.KBTrashRetention)
			items[i].PurgeAt = &purgeAt
		}
	}
	return nil
}

// GetKBMediaDetails returns a library item, in the trash or not, with its usage and every article
// that names it: live ones first, then those in the trash.
func (s *Store) GetKBMediaDetails(ctx context.Context, workspaceID, id string) (domain.KBMediaDetails, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBMediaDetails{}, domain.ErrKBMediaNotFound
	}
	details, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBMediaDetails, error) {
		media, err := scanKBMedia(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, id))
		if err != nil {
			return domain.KBMediaDetails{}, err
		}
		items := []StoredKBMedia{media}
		if err := addKBMediaUsage(ctx, tx, workspaceID, items); err != nil {
			return domain.KBMediaDetails{}, err
		}
		rows, err := tx.Query(ctx, `SELECT a.id::text,a.number,a.title,a.status,a.deleted_at IS NOT NULL,l.current
			FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 ORDER BY a.deleted_at IS NOT NULL, NOT l.current, a.number`, workspaceID, id)
		if err != nil {
			return domain.KBMediaDetails{}, err
		}
		articles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBMediaArticle, error) {
			var article domain.KBMediaArticle
			return article, row.Scan(&article.ID, &article.Number, &article.Title, &article.Status, &article.InTrash, &article.Current)
		})
		return domain.KBMediaDetails{KBMedia: items[0].KBMedia, Articles: articles}, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBMediaDetails{}, domain.ErrKBMediaNotFound
	}
	if err != nil {
		return domain.KBMediaDetails{}, fmt.Errorf("get knowledge base media details: %w", err)
	}
	return details, nil
}

// UpdateKBMedia changes the name, alt text or kept mark of a library item outside the trash. A new
// name or alt keeps the item, since a person cared for it, unless the patch sets kept itself.
// It answers domain.ErrKBChanged when the item was saved after patch.UpdatedAt.
func (s *Store) UpdateKBMedia(ctx context.Context, workspaceID string, actor Actor, id string, patch domain.KBMediaPatch) (domain.KBMedia, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBMedia{}, domain.ErrKBMediaNotFound
	}
	if patch.Name == nil && patch.Alt == nil && patch.Kept == nil {
		return domain.KBMedia{}, &domain.InvalidKBError{Message: "Send name, alt or kept to change."}
	}
	for _, field := range []struct {
		what  string
		value **string
		limit int
	}{{"name", &patch.Name, domain.MaxKBMediaName}, {"alt text", &patch.Alt, domain.MaxKBMediaAlt}} {
		if *field.value == nil {
			continue
		}
		cleaned, err := domain.CleanKBMediaText(field.what, **field.value, field.limit)
		if err != nil {
			return domain.KBMedia{}, err
		}
		*field.value = &cleaned
	}
	kept := patch.Kept
	if kept == nil && (patch.Name != nil || patch.Alt != nil) {
		keep := true
		kept = &keep
	}
	media, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBMedia, error) {
		if err := kbLiveAndCurrent(ctx, tx, `kb_media`, workspaceID, id, patch.UpdatedAt, domain.ErrKBMediaNotFound); err != nil {
			return StoredKBMedia{}, err
		}
		media, err := scanKBMedia(tx.QueryRow(ctx, `UPDATE kb_media m SET name=COALESCE($3,m.name), alt=COALESCE($4,m.alt), kept=COALESCE($5,m.kept),
				updated_at=clock_timestamp(), updated_by_agent_id=$6
			WHERE m.workspace_id=$1 AND m.id=$2 RETURNING `+kbMediaColumns,
			workspaceID, id, patch.Name, patch.Alt, kept, optionalID(actor.AgentID)))
		if err != nil {
			return StoredKBMedia{}, err
		}
		items := []StoredKBMedia{media}
		err = addKBMediaUsage(ctx, tx, workspaceID, items)
		return items[0], err
	})
	if err != nil {
		return domain.KBMedia{}, kbError("update knowledge base media", err)
	}
	return media.KBMedia, nil
}
