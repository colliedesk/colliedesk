package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbMediaEarlierVersions lists the items replacements put id in the place of, newest first. A
// replacement that was undone, or that changed a file in place (its earlier bytes are kept nowhere),
// lists nothing.
func kbMediaEarlierVersions(ctx context.Context, tx pgx.Tx, workspaceID, id string) ([]domain.KBMediaEarlierVersion, error) {
	rows, err := tx.Query(ctx, `SELECT `+kbMediaColumns+`, r.id::text, r.article_ids::text[], r.created_at
		FROM kb_media_replacements r JOIN kb_media m ON m.workspace_id=r.workspace_id AND m.id=r.from_media_id
		WHERE r.workspace_id=$1 AND r.to_media_id=$2 AND r.undone_at IS NULL AND NOT r.in_place
		ORDER BY r.created_at DESC, r.id`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	versions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBMediaEarlierVersion, error) {
		var version domain.KBMediaEarlierVersion
		media, err := scanKBMedia(row, &version.ReplacementID, &version.ArticleIDs, &version.ReplacedAt)
		version.MediaID, version.Name, version.URL, version.ReplacedAt = media.ID, media.Name, media.URL, domain.UTC(version.ReplacedAt)
		return version, err
	})
	if versions == nil {
		versions = []domain.KBMediaEarlierVersion{}
	}
	return versions, err
}

// kbMediaKeptByReplacement is true, on library rows aliased m, for an item that is the earlier version
// of a replacement not undone (kbMediaEarlierVersions): undoing it puts the item back, so no purge may
// delete it until then.
const kbMediaKeptByReplacement = `EXISTS(SELECT 1 FROM kb_media_replacements r
	WHERE r.workspace_id=m.workspace_id AND r.from_media_id=m.id AND r.undone_at IS NULL AND NOT r.in_place)`

// kbMediaKeptBy lists, for a purge's refusal, each replacement not undone that keeps one of ids, with
// the articles it put the new item in, in the order they were replaced.
func kbMediaKeptBy(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) ([]domain.KBMediaKept, error) {
	rows, err := tx.Query(ctx, `SELECT r.from_media_id::text, COALESCE(f.name,''), r.id::text, r.to_media_id::text, COALESCE(t.name,''),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('id', a.id, 'number', a.number, 'title', a.title) ORDER BY o.n)
				FROM unnest(r.article_ids) WITH ORDINALITY o(id, n) JOIN kb_articles a ON a.workspace_id=r.workspace_id AND a.id=o.id), '[]')
		FROM kb_media_replacements r
		LEFT JOIN kb_media f ON f.workspace_id=r.workspace_id AND f.id=r.from_media_id
		LEFT JOIN kb_media t ON t.workspace_id=r.workspace_id AND t.id=r.to_media_id
		WHERE r.workspace_id=$1 AND r.from_media_id=ANY($2::uuid[]) AND r.undone_at IS NULL AND NOT r.in_place
		ORDER BY r.created_at, r.id`, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBMediaKept, error) {
		var kept domain.KBMediaKept
		err := row.Scan(&kept.MediaID, &kept.Name, &kept.ReplacementID, &kept.ToMediaID, &kept.ToName, &kept.Articles)
		return kept, err
	})
}
