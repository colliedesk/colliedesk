package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The functions below write the help centers an import makes from the source's portals, in the
// importer's transaction, under the same cap and handle rules as POST /api/v1/help-centers.

// ImportPrimaryHelpCenter returns the workspace's primary help center, making it when there is none yet.
func ImportPrimaryHelpCenter(ctx context.Context, tx pgx.Tx, workspaceID string) (string, error) {
	return resolveHelpCenter(ctx, tx, workspaceID, "", true)
}

// ImportHelpCenter adds a help center named name, off until the team turns it on, in the primary's
// language. Its handle is made from the name, else fallback; a taken handle gets -2, -3. At the cap
// it returns domain.ErrHelpCenterLimit and writes nothing.
func ImportHelpCenter(ctx context.Context, tx pgx.Tx, workspaceID, name, fallback string) (id, handle string, err error) {
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > domain.MaxHelpCenterName {
		name = strings.TrimSpace(string(runes[:domain.MaxHelpCenterName]))
	}
	if name, err = domain.CleanHelpCenterName(name); err != nil || name == "" {
		return "", "", errors.Join(err, &domain.InvalidKBError{Message: "An imported help center needs a name."})
	}
	if _, err := resolveHelpCenter(ctx, tx, workspaceID, "", true); err != nil {
		return "", "", err
	}
	var locale string
	if err := tx.QueryRow(ctx, `SELECT locale FROM help_centers WHERE workspace_id=$1 AND primary_help_center FOR UPDATE`, workspaceID).Scan(&locale); err != nil {
		return "", "", err
	}
	rows, err := tx.Query(ctx, `SELECT handle FROM help_centers WHERE workspace_id=$1 AND deleted_at IS NULL AND handle IS NOT NULL`, workspaceID)
	if err != nil {
		return "", "", err
	}
	handles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", "", err
	}
	if len(handles)+1 >= domain.MaxHelpCenters {
		return "", "", domain.ErrHelpCenterLimit
	}
	taken := make(map[string]bool, len(handles))
	for _, h := range handles {
		taken[h] = true
	}
	handle = freeHelpCenterHandle(taken, name, fallback)
	if handle == "" {
		return "", "", fmt.Errorf("import help center %q: no free handle", name)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO help_centers(workspace_id, primary_help_center, name, handle, title, locale, enabled, show_tags, updated_at)
		VALUES($1, false, $2, $3, '', $4, false, false, clock_timestamp()) RETURNING id::text`,
		workspaceID, name, handle, locale).Scan(&id); err != nil {
		return "", "", err
	}
	return id, handle, bumpKB(ctx, tx, workspaceID)
}

// freeHelpCenterHandle is the first handle CheckHelpCenterHandle takes and taken lacks: the handle
// of name, then it with -help (support is a route, so Support becomes support-help), then fallback,
// each also with -2, -3 and so on.
func freeHelpCenterHandle(taken map[string]bool, name, fallback string) string {
	base := domain.HelpCenterHandleFrom(name)
	for _, base := range []string{base, domain.TrimHelpCenterHandle(base, domain.MaxHelpCenterHandle-len("-help")) + "-help", fallback} {
		if base == "-help" || domain.CheckHelpCenterHandle(base) != nil {
			continue
		}
		for n := 1; n <= domain.MaxHelpCenters+1; n++ {
			candidate := base
			if n > 1 {
				suffix := fmt.Sprintf("-%d", n)
				candidate = domain.TrimHelpCenterHandle(base, domain.MaxHelpCenterHandle-len(suffix)) + suffix
			}
			if !taken[candidate] && domain.CheckHelpCenterHandle(candidate) == nil {
				return candidate
			}
		}
	}
	return ""
}

// AddImportedKBCollectionHelpCenters also shows top-level collection id on the live help centers ids
// names, keeping where it is shown already. A nested collection is left as it is.
func AddImportedKBCollectionHelpCenters(ctx context.Context, tx pgx.Tx, workspaceID, id string, ids []string) error {
	tag, err := tx.Exec(ctx, `INSERT INTO kb_collection_help_centers(workspace_id,collection_id,help_center_id,position)
		SELECT c.workspace_id, c.id, h.id, c.position FROM kb_collections c
		JOIN help_centers h ON h.workspace_id=c.workspace_id AND h.id=ANY($3::uuid[]) AND h.deleted_at IS NULL
		WHERE c.workspace_id=$1 AND c.id=$2 AND c.parent_id IS NULL
		ON CONFLICT DO NOTHING`, workspaceID, id, ids)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

// TrashImportedHelpCenter moves help center id, made by an import, to the trash and takes its
// collections off it; a top-level collection then shown on no help center goes back to the primary.
// The primary, a help center with a custom domain and one a live channel serves are kept: trashed is
// false and nothing changes.
func TrashImportedHelpCenter(ctx context.Context, tx pgx.Tx, workspaceID, id string) (trashed bool, err error) {
	// A channel that names the help center holds its row FOR SHARE until it commits, so this lock waits
	// for it, and the next statement's snapshot sees the channel.
	var live bool
	if err := tx.QueryRow(ctx, `SELECT true FROM help_centers WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND NOT primary_help_center FOR UPDATE`,
		workspaceID, id).Scan(&live); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE help_centers h SET deleted_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE h.workspace_id=$1 AND h.id=$2
			AND NOT EXISTS(SELECT 1 FROM help_center_domains d WHERE d.workspace_id=h.workspace_id AND d.help_center_id=h.id)
			AND NOT EXISTS(SELECT 1 FROM channels c WHERE c.workspace_id=h.workspace_id AND c.help_center_id=h.id AND c.deleted_at IS NULL)`, workspaceID, id)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `WITH off AS (DELETE FROM kb_collection_help_centers WHERE workspace_id=$1 AND help_center_id=$2 RETURNING collection_id)
		INSERT INTO kb_collection_help_centers(workspace_id,collection_id,help_center_id,position)
		SELECT c.workspace_id, c.id, primary_help_center_id(c.workspace_id), c.position FROM kb_collections c
		WHERE c.workspace_id=$1 AND c.id IN (SELECT collection_id FROM off) AND NOT EXISTS(
			SELECT 1 FROM kb_collection_help_centers p JOIN help_centers h ON h.workspace_id=p.workspace_id AND h.id=p.help_center_id AND h.deleted_at IS NULL
			WHERE p.workspace_id=c.workspace_id AND p.collection_id=c.id AND p.help_center_id<>$2)
		ON CONFLICT DO NOTHING`, workspaceID, id); err != nil {
		return false, err
	}
	return true, bumpKB(ctx, tx, workspaceID)
}
