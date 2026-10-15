package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const dateLayout = "2006-01-02"

func loadHistory(ctx context.Context, tx pgx.Tx, id string) ([]domain.Subscription, error) {
	return store.LoadAccessHistory(ctx, tx, id)
}

// notify mails the admins of the workspace that have no record yet, from the platform address and
// never through the workspace's own channel, and records each admin as it is reached (or refused
// for good by the relay). The 30 day clock (retention_notice_sent_at) starts when no admin is left
// to mail and at least one was reached, because deletion relies on it. A transient failure leaves
// the rest for the next pass and is returned as soft: what was recorded still commits.
func (s *Service) notify(ctx context.Context, tx pgx.Tx, st state, now time.Time) (Action, error, error) {
	if s.config.Mailer == nil || s.config.From == "" {
		return ActionNone, nil, ErrNoMailer
	}
	// Records of an earlier lock do not count.
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_retention_notices WHERE workspace_id=$1 AND sent_at < $2`, st.WorkspaceID, st.LockedSince); err != nil {
		return ActionNone, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text, a.email FROM agents a WHERE a.workspace_id=$1 AND a.role='admin'
		AND NOT EXISTS (SELECT 1 FROM workspace_retention_notices n WHERE n.workspace_id=a.workspace_id AND n.agent_id=a.id)
		ORDER BY a.email`, st.WorkspaceID)
	if err != nil {
		return ActionNone, nil, err
	}
	type admin struct{ ID, Email string }
	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (admin, error) {
		var a admin
		return a, row.Scan(&a.ID, &a.Email)
	})
	if err != nil {
		return ActionNone, nil, err
	}
	var transient []error
	for _, a := range pending {
		message, err := s.compose(st, a.Email, now)
		if err != nil {
			return ActionNone, nil, err
		}
		status := "sent"
		switch err := s.config.Mailer.Send(ctx, s.config.From, []string{a.Email}, message); {
		case err == nil:
		case email.IsPermanent(err):
			status = "rejected"
			s.config.Logger.Warn("retention notice rejected for good", "workspace_id", st.WorkspaceID, "slug", st.Slug)
		default:
			transient = append(transient, fmt.Errorf("send notice: %w", err))
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_retention_notices(workspace_id,agent_id,status,sent_at) VALUES($1,$2,$3,$4)`,
			st.WorkspaceID, a.ID, status, now); err != nil {
			return ActionNone, nil, err
		}
	}
	if len(transient) > 0 {
		return ActionNone, errors.Join(transient...), nil
	}
	var delivered int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_retention_notices WHERE workspace_id=$1 AND status='sent'`, st.WorkspaceID).Scan(&delivered); err != nil {
		return ActionNone, nil, err
	}
	if delivered == 0 {
		s.config.Logger.Warn("retention notice reached no admin; the workspace will not be deleted", "workspace_id", st.WorkspaceID, "slug", st.Slug)
		return ActionNone, nil, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET retention_notice_sent_at=$2 WHERE id=$1`, st.WorkspaceID, now); err != nil {
		return ActionNone, nil, err
	}
	s.config.Logger.Info("retention notice sent", "workspace_id", st.WorkspaceID, "slug", st.Slug, "delivered", delivered,
		"delete_on", earliestDeletion(st.LockedSince, now).Format(dateLayout))
	return ActionNotice, nil, nil
}

func (s *Service) compose(st state, to string, now time.Time) ([]byte, error) {
	domainPart := s.config.From[strings.LastIndex(s.config.From, "@")+1:]
	id, err := email.NewMessageID(domainPart)
	if err != nil {
		return nil, err
	}
	date := earliestDeletion(st.LockedSince, now).Format(dateLayout)
	body := fmt.Sprintf(`The Colliedesk workspace "%s" has been locked since %s: its trial or subscription ended.

Colliedesk deletes a workspace that stays locked for 12 months. Yours will be deleted on %s (UTC), with all its conversations, contacts and attachments. It cannot be recovered after that.

To keep it, choose a plan in Settings > Billing: %s
To keep a copy of your data, export all your data from Colliedesk (Settings, or GET /api/v1/export) before %s.

You get this message because you are an admin of the workspace.
`, st.Name, st.LockedSince.Format(dateLayout), date, s.config.Origin, date)
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: s.config.From, ToAddress: to,
		Subject: fmt.Sprintf("Your Colliedesk workspace %s will be deleted on %s", st.Name, date), Body: body,
		MessageID: id, Date: now, Standalone: true})
}
