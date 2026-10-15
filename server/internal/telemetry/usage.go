package telemetry

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// usage is what the report counts across the whole install.
type usage struct {
	installID       string
	workspaces      int64
	activeAgents    int64 // distinct agents with a session seen in the last 30 days
	replies         int64 // outgoing messages created in the last 30 days
	emailChannel    bool
	webForm         bool
	freshdeskImport bool
}

// collectUsage reads the install id and the counts. Only aggregates leave the transaction.
func collectUsage(ctx context.Context, db *database.DB) (usage, error) {
	var u usage
	err := db.Privileged(ctx, database.PrivilegeTelemetry, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
			(SELECT install_id::text FROM instance),
			(SELECT count(*) FROM workspaces),
			(SELECT count(DISTINCT (workspace_id, agent_id)) FROM sessions WHERE last_seen_at >= now() - interval '30 days'),
			(SELECT count(*) FROM messages WHERE kind = 'outgoing' AND created_at >= now() - interval '30 days'),
			EXISTS (SELECT 1 FROM email_channels),
			EXISTS (SELECT 1 FROM web_form_channels WHERE enabled),
			EXISTS (SELECT 1 FROM import_items WHERE source LIKE 'freshdesk:%')`).
			Scan(&u.installID, &u.workspaces, &u.activeAgents, &u.replies, &u.emailChannel, &u.webForm, &u.freshdeskImport)
	})
	return u, err
}
