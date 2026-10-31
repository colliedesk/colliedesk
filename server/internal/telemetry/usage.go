package telemetry

import (
	"context"
	"errors"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// usage is what the report counts across the whole install.
type usage struct {
	installID       string
	workspaces      int64
	activeAgents    int64 // distinct agents with a session seen in the last 30 days
	replies         int64 // replies sent from Colliedesk (not imported) in the last 30 days
	emailChannel    bool
	webForm         bool
	freshdeskImport bool
	// snapshots is one per workspace; read only on Colliedesk Cloud.
	snapshots []snapshot
}

// claimUsage takes this day's report for the install and reads the counts. It returns false when
// a report was already taken in the last 23 hours, by this process or another one on the same
// database (a restart, a crash loop, a second replica), so an install reports at most once a day.
// The claim is taken before sending: a failed send skips that day rather than retrying.
// On Colliedesk Cloud the per-workspace snapshots are read in the same transaction.
// Only aggregates leave the transaction.
func claimUsage(ctx context.Context, db *database.DB, cloud bool) (usage, bool, error) {
	var u usage
	claimed := false
	err := db.Privileged(ctx, database.PrivilegeTelemetry, func(tx pgx.Tx) error {
		// The reply count reads every message of the last 30 days. No index serves it: one would
		// cost a write on every message for a count taken once a day. The timeout bounds it.
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '10s'`); err != nil {
			return err
		}
		// A restored or hand-edited database may have lost the row; a new id is better than none.
		if _, err := tx.Exec(ctx, `INSERT INTO instance DEFAULT VALUES ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE instance SET last_reported_at = now()
			WHERE last_reported_at IS NULL OR last_reported_at < now() - interval '23 hours'
			RETURNING install_id::text`).Scan(&u.installID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = true
		// Replies imported from another helpdesk keep their original time; only replies sent here count.
		err = tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM workspaces),
			(SELECT count(DISTINCT (workspace_id, agent_id)) FROM sessions WHERE last_seen_at >= now() - interval '30 days'),
			(SELECT count(*) FROM messages WHERE kind = 'outgoing' AND origin IS NULL AND notice_kind IS NULL AND created_at >= now() - interval '30 days'),
			EXISTS (SELECT 1 FROM email_channels),
			EXISTS (SELECT 1 FROM web_form_channels f JOIN channels c ON c.workspace_id=f.workspace_id AND c.id=f.channel_id WHERE c.enabled),
			EXISTS (SELECT 1 FROM import_items WHERE source LIKE 'freshdesk:%')`).
			Scan(&u.workspaces, &u.activeAgents, &u.replies, &u.emailChannel, &u.webForm, &u.freshdeskImport)
		if err != nil || !cloud {
			return err
		}
		u.snapshots, err = readSnapshots(ctx, tx, time.Now())
		return err
	})
	return u, claimed, err
}
