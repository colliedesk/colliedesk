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
