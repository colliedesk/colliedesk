package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SLAAlertDue is a reminder or overdue email a running clock reached and nobody sent yet.
type SLAAlertDue struct {
	WorkspaceID, ConversationID string
	Clock, Threshold            string
	DueAt                       time.Time
}

// SLAAlert is an alert with who gets it.
type SLAAlert struct {
	SLAAlertDue
	Slug, WorkspaceName, Timezone string
	Subject, DisplayRef           string
	All                           []string
}

// DueSLAAlerts lists, in every workspace whose targets are on with emails, the thresholds running clocks
// reached by now that are not done: overdue once due (never a reminder too), a reminder once it warns. A
// threshold from before the conversation came in sends nothing.
func (s *Store) DueSLAAlerts(ctx context.Context, now time.Time, limit int) ([]SLAAlertDue, error) {
	var due []SLAAlertDue
	err := s.db.Privileged(ctx, database.PrivilegeSLAClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.workspace_id::text,c.id::text,v.clock,th.threshold,v.due FROM conversations c
			JOIN response_targets t ON t.workspace_id=c.workspace_id AND t.enabled AND t.alert_email
			CROSS JOIN LATERAL (VALUES ('first_response',c.sla_first_state,c.sla_first_due_at,c.sla_first_warn_at),
				('resolution',c.sla_resolution_state,c.sla_resolution_due_at,c.sla_resolution_warn_at)) v(clock,state,due,warn)
			CROSS JOIN LATERAL (SELECT CASE WHEN v.due<=$1 THEN 'overdue' WHEN v.warn<=$1 THEN 'reminder' END AS threshold,
				CASE WHEN v.due<=$1 THEN v.due ELSE v.warn END AS reached) th
			WHERE c.sla_warn_at<=$1 AND c.sla_applies AND NOT c.sla_stale AND v.state='running' AND th.threshold IS NOT NULL
				AND th.reached>=COALESCE(c.sla_alert_since,c.created_at)
				AND NOT EXISTS(SELECT 1 FROM conversation_sla_alerts a WHERE a.workspace_id=c.workspace_id AND a.conversation_id=c.id
					AND a.clock=v.clock AND a.threshold=th.threshold AND a.due_at=v.due AND a.recipient='')
			ORDER BY th.reached LIMIT $2`, now, limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SLAAlertDue, error) {
			var item SLAAlertDue
			err := row.Scan(&item.WorkspaceID, &item.ConversationID, &item.Clock, &item.Threshold, &item.DueAt)
			item.DueAt = item.DueAt.UTC()
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list due response alerts: %w", err)
	}
	return due, nil
}

// closeSettled marks a threshold done once everyone who gets it has their email settled (sent or rejected).
const closeSettled = `INSERT INTO conversation_sla_alerts(workspace_id,conversation_id,clock,threshold,due_at,recipient,status)
	SELECT $1,$2,$3,$4,$5,'','done' WHERE (SELECT count(*) FROM conversation_sla_alerts WHERE workspace_id=$1 AND conversation_id=$2 AND clock=$3 AND threshold=$4
		AND due_at=$5 AND recipient=ANY($6) AND status IN ('sent','rejected'))=cardinality($6::text[])
	ON CONFLICT DO NOTHING`

// PrepareSLAAlert finds who gets an alert: the assignee, or the admins while nobody is assigned, each
// unless they turned these emails off. Nobody to tell, or everyone told already (the recipients shrank
// after some got it), closes the threshold.
func (s *Store) PrepareSLAAlert(ctx context.Context, due SLAAlertDue) (SLAAlert, error) {
	alert := SLAAlert{SLAAlertDue: due}
	_, err := scoped(ctx, s, due.WorkspaceID, func(tx pgx.Tx) (struct{}, error) {
		var assignee *string
		if err := tx.QueryRow(ctx, `SELECT w.slug,w.name,COALESCE(t.timezone,w.timezone),c.subject,c.display_ref,c.assignee_id::text
			FROM conversations c JOIN workspaces w ON w.id=c.workspace_id LEFT JOIN response_targets t ON t.workspace_id=c.workspace_id
			WHERE c.workspace_id=$1 AND c.id=$2`, due.WorkspaceID, due.ConversationID).Scan(&alert.Slug, &alert.WorkspaceName, &alert.Timezone, &alert.Subject,
			&alert.DisplayRef, &assignee); err != nil {
			return struct{}{}, fmt.Errorf("read response alert: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT email FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL AND sla_alert_email
			AND CASE WHEN $2::uuid IS NULL THEN role='admin' ELSE id=$2::uuid END ORDER BY email`, due.WorkspaceID, assignee)
		if err != nil {
			return struct{}{}, fmt.Errorf("read response alert recipients: %w", err)
		}
		if alert.All, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return struct{}{}, err
		}
		if len(alert.All) == 0 {
			_, err := tx.Exec(ctx, `INSERT INTO conversation_sla_alerts(workspace_id,conversation_id,clock,threshold,due_at,recipient,status) VALUES($1,$2,$3,$4,$5,'','nobody')
				ON CONFLICT DO NOTHING`, due.WorkspaceID, due.ConversationID, due.Clock, due.Threshold, due.DueAt)
			return struct{}{}, err
		}
		_, err = tx.Exec(ctx, closeSettled, due.WorkspaceID, due.ConversationID, due.Clock, due.Threshold, due.DueAt, alert.All)
		return struct{}{}, err
	})
	return alert, err
}

// ClaimSLARecipient claims one recipient's email right before it goes, unless another server is sending
// it or it went; a claim left sending longer than abandoned is taken again.
func (s *Store) ClaimSLARecipient(ctx context.Context, alert SLAAlert, to string, abandoned time.Duration) (bool, error) {
	return scoped(ctx, s, alert.WorkspaceID, func(tx pgx.Tx) (bool, error) {
		var claimed bool
		err := tx.QueryRow(ctx, `INSERT INTO conversation_sla_alerts(workspace_id,conversation_id,clock,threshold,due_at,recipient) VALUES($1,$2,$3,$4,$5,$6)
			ON CONFLICT (workspace_id,conversation_id,clock,threshold,due_at,recipient) DO UPDATE SET created_at=now()
			WHERE conversation_sla_alerts.status='sending' AND conversation_sla_alerts.created_at<now()-make_interval(secs => $7)
			RETURNING true`, alert.WorkspaceID, alert.ConversationID, alert.Clock, alert.Threshold, alert.DueAt, to, abandoned.Seconds()).Scan(&claimed)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("claim response alert: %w", err)
		}
		return true, nil
	})
}

// SettleSLAAlert records how one recipient's email ended: sent or rejected; "" gives the claim back for a
// later round (a send that failed for now). Once everyone's is settled, the threshold is done.
func (s *Store) SettleSLAAlert(ctx context.Context, alert SLAAlert, recipient, status string) error {
	_, err := scoped(ctx, s, alert.WorkspaceID, func(tx pgx.Tx) (struct{}, error) {
		key := []any{alert.WorkspaceID, alert.ConversationID, alert.Clock, alert.Threshold, alert.DueAt, recipient}
		if status == "" {
			_, err := tx.Exec(ctx, `DELETE FROM conversation_sla_alerts WHERE workspace_id=$1 AND conversation_id=$2 AND clock=$3 AND threshold=$4 AND due_at=$5 AND recipient=$6`, key...)
			return struct{}{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE conversation_sla_alerts SET status=$7 WHERE workspace_id=$1 AND conversation_id=$2 AND clock=$3 AND threshold=$4 AND due_at=$5 AND recipient=$6`,
			append(key, status)...); err != nil {
			return struct{}{}, err
		}
		_, err := tx.Exec(ctx, closeSettled, alert.WorkspaceID, alert.ConversationID, alert.Clock, alert.Threshold, alert.DueAt, alert.All)
		return struct{}{}, err
	})
	return err
}

// SetMySLAAlertEmail turns a teammate's own reminder and overdue emails on or off.
func (s *Store) SetMySLAAlertEmail(ctx context.Context, workspaceID, agentID string, on bool) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE agents SET sla_alert_email=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, on)
		return struct{}{}, err
	})
	return err
}

func readSLAAlerts(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (map[string][]domain.SLAAlertRecord, error) {
	rows, err := tx.Query(ctx, `SELECT conversation_id::text,clock,threshold,due_at,
			COALESCE(max(status) FILTER (WHERE recipient=''),'sending'),count(*) FILTER (WHERE status='sent'),
			bool_or(status='rejected'),min(created_at)
		FROM conversation_sla_alerts WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[])
		GROUP BY conversation_id,clock,threshold,due_at ORDER BY min(created_at),clock`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read response alerts: %w", err)
	}
	defer rows.Close()
	alerts := map[string][]domain.SLAAlertRecord{}
	for rows.Next() {
		var id string
		var rejected bool
		var record domain.SLAAlertRecord
		if err := rows.Scan(&id, &record.Clock, &record.Threshold, &record.DueAt, &record.Status, &record.Recipients, &rejected, &record.At); err != nil {
			return nil, err
		}
		if record.Status == "done" {
			record.Status = "sent"
			if record.Recipients == 0 && rejected {
				record.Status = "rejected"
			}
		}
		record.DueAt, record.At = record.DueAt.UTC(), record.At.UTC()
		alerts[id] = append(alerts[id], record)
	}
	return alerts, rows.Err()
}

// MySLAAlertEmail says whether a teammate gets the reminder and overdue emails.
func (s *Store) MySLAAlertEmail(ctx context.Context, workspaceID, agentID string) (bool, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var on bool
		err := tx.QueryRow(ctx, `SELECT sla_alert_email FROM agents WHERE workspace_id=$1 AND id::text=$2`, workspaceID, agentID).Scan(&on)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return on, err
	})
}
