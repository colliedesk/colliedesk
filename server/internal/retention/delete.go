package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// remove deletes one workspace that Decide marked for deletion, under the workspace row lock. It
// holds the deletion back, without error, when deletion is switched off or the daily cap is
// reached; with an error when the billing provider cannot confirm that nobody pays.
//
// Order matters: the files are moved to the trash (one atomic rename), then the row is deleted and
// the deletion recorded. The returned undo moves the files back; the caller runs it when this
// transaction does not commit. The trash is purged after TrashWindow, so a mistake stays
// recoverable for that long.
func (s *Service) remove(ctx context.Context, tx pgx.Tx, st state, now time.Time) (done Action, held bool, undo func() error, err error) {
	if !s.config.DeleteEnabled {
		s.config.Logger.Warn("retention would delete this workspace; deletion is switched off (RETENTION_DELETE)", "workspace_id", st.WorkspaceID, "slug", st.Slug)
		return ActionNone, true, nil, nil
	}
	// One deletion decision at a time, so the cap holds across workspaces.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('retention-delete',0))`); err != nil {
		return ActionNone, false, nil, err
	}
	var today int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM retention_deletions WHERE deleted_at > $1`, now.Add(-24*time.Hour)).Scan(&today); err != nil {
		return ActionNone, false, nil, err
	}
	if today >= s.config.MaxDeletionsPerDay {
		s.config.Logger.Error("retention deletion cap reached; workspace kept", "workspace_id", st.WorkspaceID, "slug", st.Slug,
			"deleted_last_24h", today, "max_per_day", s.config.MaxDeletionsPerDay)
		return ActionNone, true, nil, nil
	}
	if s.config.Polar == nil {
		return ActionNone, true, nil, errors.New("no billing provider configured to confirm the workspace pays nothing; workspace kept")
	}
	paying, err := s.config.Polar.HasPaidSubscription(ctx, st.WorkspaceID, now)
	switch {
	case err != nil:
		return ActionNone, true, nil, fmt.Errorf("billing provider check failed; workspace kept: %w", err)
	case paying:
		return ActionNone, true, nil, errors.New("the billing provider reports a paid subscription that this database does not know; workspace kept")
	}
	var conversations, messages, files, agents int
	var bytes int64
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM conversations WHERE workspace_id=$1),
		(SELECT count(*) FROM messages WHERE workspace_id=$1),
		(SELECT count(*) FROM attachments WHERE workspace_id=$1),
		(SELECT COALESCE(sum(size),0) FROM attachments WHERE workspace_id=$1),
		(SELECT count(*) FROM agents WHERE workspace_id=$1)`, st.WorkspaceID).
		Scan(&conversations, &messages, &files, &bytes, &agents); err != nil {
		return ActionNone, false, nil, err
	}
	var found bool
	if undo, found, err = s.config.Storage.Trash(ctx, st.WorkspaceID, now); err != nil {
		return ActionNone, false, nil, err
	}
	if !found && files > 0 {
		// The database lists files the storage does not have: most likely ATTACHMENTS_DIR points at
		// the wrong place. Deleting the row now would leave the real files behind for good.
		return ActionNone, false, nil, fmt.Errorf("the database lists %d attachment(s) but the workspace directory is missing from the attachment storage; check ATTACHMENTS_DIR; workspace kept", files)
	}
	// The domain rows go with the workspace, so the provider domains they name are queued first; the
	// queue outlives the workspace, and a sweep deletes the domains at the provider.
	if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id)
		SELECT workspace_id,provider_domain_id FROM email_domains WHERE workspace_id=$1 AND provider_domain_id IS NOT NULL
		ON CONFLICT DO NOTHING`, st.WorkspaceID); err != nil {
		return ActionNone, false, undo, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, st.WorkspaceID); err != nil {
		return ActionNone, false, undo, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO retention_deletions(deleted_workspace_id,slug,deleted_at) VALUES($1,$2,$3)`, st.WorkspaceID, st.Slug, now); err != nil {
		return ActionNone, false, undo, err
	}
	s.config.Logger.Info("workspace deleted after 12 months locked", "workspace_id", st.WorkspaceID, "slug", st.Slug,
		"locked_since", st.LockedSince.Format(dateLayout), "conversations", conversations, "messages", messages,
		"attachments", files, "attachment_bytes", bytes, "agents", agents)
	return ActionDelete, false, undo, nil
}
