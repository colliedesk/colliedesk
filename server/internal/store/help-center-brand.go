package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// HelpCenterBrandImage returns the logo or favicon the help center shows: an image of the workspace's
// library, outside its trash, that the help center names now. Anything else, an image the theme named
// before included, is domain.ErrKBImageNotFound.
func (s *Store) HelpCenterBrandImage(ctx context.Context, workspaceID, id string) (StoredKBImage, error) {
	if !savedReplyID.MatchString(id) || id != strings.ToLower(id) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) {
		return scanKBImage(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2
			AND m.kind='image' AND m.deleted_at IS NULL AND `+kbMediaNamedByHelpCenter, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	if err != nil {
		return StoredKBImage{}, fmt.Errorf("get help center brand image: %w", err)
	}
	return image, nil
}
