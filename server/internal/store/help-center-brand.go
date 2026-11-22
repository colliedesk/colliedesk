package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// HelpCenterBrandImage returns the logo or favicon help center helpCenterID ("" the primary one) shows:
// an image of the workspace's library, outside its trash, that this help center names now. Anything
// else, an image the theme named before or another help center's logo included, is
// domain.ErrKBImageNotFound.
func (s *Store) HelpCenterBrandImage(ctx context.Context, workspaceID, helpCenterID, id string) (StoredKBImage, error) {
	if !savedReplyID.MatchString(id) || id != strings.ToLower(id) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	image, err := helpCenterScoped(ctx, s, workspaceID, helpCenterID, func(tx pgx.Tx, helpCenterID string) (StoredKBImage, error) {
		query, args := onHelpCenter(helpCenterID, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2
			AND m.kind='image' AND m.deleted_at IS NULL AND EXISTS(SELECT 1 FROM help_centers h
				WHERE h.workspace_id=m.workspace_id AND h.id=$H AND (h.logo_media_id=m.id OR h.favicon_media_id=m.id))`, workspaceID, id)
		return scanKBImage(tx.QueryRow(ctx, query, args...))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	if err != nil {
		return StoredKBImage{}, fmt.Errorf("get help center brand image: %w", err)
	}
	return image, nil
}
