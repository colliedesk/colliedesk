package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// helpCenterSummaryQuery reads the live help centers of workspace $1, primary first; require_approval
// and content_version are the knowledge base's, kept on the primary row.
const helpCenterSummaryQuery = `SELECT h.id::text, h.name, h.handle, h.primary_help_center, h.enabled, h.title, h.locale,
		p.require_approval, h.show_tags, p.content_version, h.updated_at, COALESCE(d.hostname, '')
	FROM help_centers h
	JOIN help_centers p ON p.workspace_id=h.workspace_id AND p.primary_help_center
	LEFT JOIN help_center_domains d ON d.workspace_id=h.workspace_id AND d.help_center_id=h.id AND d.status='active'
	WHERE h.workspace_id=$1 AND h.deleted_at IS NULL`

func scanHelpCenterSummary(row pgx.Row) (domain.HelpCenterSummary, error) {
	var h domain.HelpCenterSummary
	err := row.Scan(&h.ID, &h.Name, &h.Handle, &h.Primary, &h.Enabled, &h.Title, &h.Locale,
		&h.RequireApproval, &h.ShowTags, &h.ContentVersion, &h.UpdatedAt, &h.Hostname)
	h.UpdatedAt = domain.UTCOptional(h.UpdatedAt)
	return h, err
}

// resolveHelpCenter is the id of help center helpCenterID of the workspace, "" its primary one. A
// write (ensure) makes the primary when the workspace has none yet; a read gets "" for it. Any other
// id must be a live help center of the workspace, else domain.ErrHelpCenterNotFound.
func resolveHelpCenter(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string, ensure bool) (string, error) {
	if helpCenterID == "" {
		var id *string
		query := `SELECT primary_help_center_id($1)::text`
		if ensure {
			query = `SELECT ensure_primary_help_center($1)::text`
		}
		if err := tx.QueryRow(ctx, query, workspaceID).Scan(&id); err != nil || id == nil {
			return "", err
		}
		return *id, nil
	}
	if !savedReplyID.MatchString(helpCenterID) {
		return "", domain.ErrHelpCenterNotFound
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM help_centers WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, workspaceID, helpCenterID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrHelpCenterNotFound
	}
	return id, err
}

// ListHelpCenters returns the live help centers of the workspace, the primary first and the others by
// handle. A workspace without one gets its primary (off), so the list always names it.
func (s *Store) ListHelpCenters(ctx context.Context, workspaceID string) ([]domain.HelpCenterSummary, error) {
	list, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.HelpCenterSummary, error) {
		if _, err := resolveHelpCenter(ctx, tx, workspaceID, "", true); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, helpCenterSummaryQuery+` ORDER BY h.primary_help_center DESC, h.handle`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.HelpCenterSummary, error) { return scanHelpCenterSummary(row) })
	})
	if err != nil {
		return nil, fmt.Errorf("list help centers: %w", err)
	}
	return list, nil
}

// GetHelpCenterByID returns one live help center of the workspace, else domain.ErrHelpCenterNotFound.
func (s *Store) GetHelpCenterByID(ctx context.Context, workspaceID, id string) (domain.HelpCenterSummary, error) {
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterSummary, error) {
		return readHelpCenterSummary(ctx, tx, workspaceID, id)
	})
	return got, helpCenterStoreError("get help center", err)
}

func readHelpCenterSummary(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.HelpCenterSummary, error) {
	if !savedReplyID.MatchString(id) {
		return domain.HelpCenterSummary{}, domain.ErrHelpCenterNotFound
	}
	got, err := scanHelpCenterSummary(tx.QueryRow(ctx, helpCenterSummaryQuery+` AND h.id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterSummary{}, domain.ErrHelpCenterNotFound
	}
	return got, err
}

// CreateHelpCenter adds a help center served below the primary's address at /<handle>. The primary's
// row is locked while the live help centers are counted, so two creates never pass the cap together.
func (s *Store) CreateHelpCenter(ctx context.Context, workspaceID string, input domain.NewHelpCenter) (domain.HelpCenterSummary, error) {
	name, err := domain.CleanHelpCenterName(input.Name)
	if err != nil {
		return domain.HelpCenterSummary{}, err
	}
	if name == "" {
		return domain.HelpCenterSummary{}, &domain.InvalidKBError{Message: "A help center needs a name, such as Hardware, so the team can tell it apart."}
	}
	title, err := domain.CleanHelpCenterTitle(input.Title)
	if err != nil {
		return domain.HelpCenterSummary{}, err
	}
	if err := domain.CheckHelpCenterHandle(input.Handle); err != nil {
		return domain.HelpCenterSummary{}, err
	}
	if err := domain.CheckLocale(input.Locale); err != nil {
		return domain.HelpCenterSummary{}, err
	}
	created, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterSummary, error) {
		if _, err := resolveHelpCenter(ctx, tx, workspaceID, "", true); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM help_centers WHERE workspace_id=$1 AND primary_help_center FOR UPDATE`, workspaceID); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		var live int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM help_centers WHERE workspace_id=$1 AND deleted_at IS NULL`, workspaceID).Scan(&live); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		if live >= domain.MaxHelpCenters {
			return domain.HelpCenterSummary{}, domain.ErrHelpCenterLimit
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO help_centers(workspace_id, primary_help_center, name, handle, title, locale, enabled, show_tags, updated_at)
			VALUES($1, false, $2, $3, $4, $5, $6, $7, clock_timestamp()) RETURNING id::text`,
			workspaceID, name, input.Handle, title, input.Locale, input.Enabled, input.ShowTags).Scan(&id); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterSummary{}, err
		}
		return readHelpCenterSummary(ctx, tx, workspaceID, id)
	})
	return created, helpCenterStoreError("create help center", err)
}

// DeleteHelpCenter moves a help center other than the primary to the trash: its pages go off line, its
// handle is free again, and its custom domain is removed and returned so the caller can forget it.
func (s *Store) DeleteHelpCenter(ctx context.Context, workspaceID, id string) (released []domain.HelpCenterDomain, err error) {
	released, err = scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.HelpCenterDomain, error) {
		if !savedReplyID.MatchString(id) {
			return nil, domain.ErrHelpCenterNotFound
		}
		var primary bool
		err := tx.QueryRow(ctx, `SELECT primary_help_center FROM help_centers WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, workspaceID, id).Scan(&primary)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrHelpCenterNotFound
		}
		if err != nil {
			return nil, err
		}
		if primary {
			return nil, domain.ErrPrimaryHelpCenter
		}
		// A channel that names the help center holds its row FOR SHARE until it commits, so the row lock
		// above has waited for it and the channel is seen here.
		serving, err := tx.Query(ctx, `SELECT id::text,name FROM channels WHERE workspace_id=$1 AND help_center_id=$2 AND deleted_at IS NULL ORDER BY name,id`, workspaceID, id)
		if err != nil {
			return nil, err
		}
		inUse, err := pgx.CollectRows(serving, pgx.RowToStructByPos[domain.ChannelName])
		if err != nil {
			return nil, err
		}
		if len(inUse) > 0 {
			return nil, &domain.HelpCenterInUseError{Channels: inUse}
		}
		if _, err := tx.Exec(ctx, `UPDATE help_centers SET deleted_at=clock_timestamp(), updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `DELETE FROM help_center_domains WHERE workspace_id=$1 AND help_center_id=$2 RETURNING `+helpCenterDomainColumns, workspaceID, id)
		if err != nil {
			return nil, err
		}
		domains, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.HelpCenterDomain, error) { return scanHelpCenterDomain(row) })
		if err != nil {
			return nil, err
		}
		return domains, bumpKB(ctx, tx, workspaceID)
	})
	return released, helpCenterStoreError("delete help center", err)
}

// helpCenterStoreError passes the help center errors a caller answers through and wraps the rest. A
// handle another live help center has is domain.ErrHelpCenterHandleTaken.
func helpCenterStoreError(what string, err error) error {
	var invalid *domain.InvalidKBError
	var violation *pgconn.PgError
	var inUse *domain.HelpCenterInUseError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &inUse):
		return err
	case errors.As(err, &violation) && violation.Code == uniqueViolation && violation.ConstraintName == "help_centers_handle":
		return domain.ErrHelpCenterHandleTaken
	case errors.Is(err, domain.ErrHelpCenterNotFound), errors.Is(err, domain.ErrHelpCenterLimit), errors.Is(err, domain.ErrPrimaryHelpCenter),
		errors.Is(err, domain.ErrHelpCenterHandleTaken), errors.As(err, &invalid):
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}
