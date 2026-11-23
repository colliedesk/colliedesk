package store

import (
	"context"
	"errors"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// PatchHelpCenter changes the fields of patch that are set on one live help center of the workspace.
// The primary keeps no handle; a language listed among the help center's other languages cannot become
// its primary one. A change bumps the knowledge base's content version, since every page shows it.
func (s *Store) PatchHelpCenter(ctx context.Context, workspaceID, id string, patch domain.HelpCenterPatch) (domain.HelpCenterSummary, error) {
	name, title, err := cleanHelpCenterPatch(patch)
	if err != nil {
		return domain.HelpCenterSummary{}, err
	}
	changed, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterSummary, error) {
		if !savedReplyID.MatchString(id) {
			return domain.HelpCenterSummary{}, domain.ErrHelpCenterNotFound
		}
		var primary bool
		err := tx.QueryRow(ctx, `SELECT primary_help_center FROM help_centers WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, workspaceID, id).Scan(&primary)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.HelpCenterSummary{}, domain.ErrHelpCenterNotFound
		}
		if err != nil {
			return domain.HelpCenterSummary{}, err
		}
		if primary && patch.Handle != nil {
			return domain.HelpCenterSummary{}, &domain.InvalidKBError{Message: "The primary help center is served at the root of the workspace's address and has no handle; give a handle to another help center."}
		}
		if patch.Locale != nil {
			var listed bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM help_center_languages WHERE workspace_id=$1 AND help_center_id=$2 AND lower(locale)=lower($3))`,
				workspaceID, id, *patch.Locale).Scan(&listed); err != nil {
				return domain.HelpCenterSummary{}, err
			}
			if listed {
				return domain.HelpCenterSummary{}, &domain.InvalidKBError{Message: *patch.Locale + " is one of this help center's other languages. Remove it there first (PUT /api/v1/help-center/languages), then make it the primary language."}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE help_centers SET name=COALESCE($3,name), handle=COALESCE($4,handle), title=COALESCE($5,title),
				locale=COALESCE($6,locale), enabled=COALESCE($7,enabled), show_tags=COALESCE($8,show_tags), updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, name, patch.Handle, title, patch.Locale, patch.Enabled, patch.ShowTags); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		return readHelpCenterSummary(ctx, tx, workspaceID, id)
	})
	return changed, helpCenterStoreError("patch help center", err)
}

// cleanHelpCenterPatch checks the fields of patch that are set and returns its cleaned name and title.
func cleanHelpCenterPatch(patch domain.HelpCenterPatch) (name, title *string, err error) {
	if patch.Name != nil {
		cleaned, err := domain.CleanHelpCenterName(*patch.Name)
		if err != nil {
			return nil, nil, err
		}
		if cleaned == "" {
			return nil, nil, &domain.InvalidKBError{Message: "A help center's name cannot be empty."}
		}
		name = &cleaned
	}
	if patch.Title != nil {
		cleaned, err := domain.CleanHelpCenterTitle(*patch.Title)
		if err != nil {
			return nil, nil, err
		}
		title = &cleaned
	}
	if patch.Handle != nil {
		if err := domain.CheckHelpCenterHandle(*patch.Handle); err != nil {
			return nil, nil, err
		}
	}
	if patch.Locale != nil {
		if err := domain.CheckLocale(*patch.Locale); err != nil {
			return nil, nil, err
		}
	}
	return name, title, nil
}
