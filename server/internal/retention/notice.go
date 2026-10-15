package retention

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

const dateLayout = "2006-01-02"

// notify mails every admin of the workspace from the platform address, never through the
// workspace's own channel, and records the notice. A transient mail failure aborts (nothing is
// recorded, the next run tries again); an address the relay rejects for good is skipped. The notice
// is recorded only when at least one admin received it, because deletion relies on it.
func (s *Service) notify(ctx context.Context, tx pgx.Tx, st state, now time.Time) (Action, error) {
	if s.config.Mailer == nil || s.config.From == "" {
		return ActionNone, ErrNoMailer
	}
	rows, err := tx.Query(ctx, `SELECT email FROM agents WHERE workspace_id=$1 AND role='admin' ORDER BY email`, st.WorkspaceID)
	if err != nil {
		return ActionNone, err
	}
	admins, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return ActionNone, err
	}
	delivered := 0
	for _, admin := range admins {
		message, err := s.compose(st, admin, now)
		if err != nil {
			return ActionNone, err
		}
		err = s.config.Mailer.Send(ctx, s.config.From, []string{admin}, message)
		switch {
		case err == nil:
			delivered++
		case email.IsPermanent(err):
			s.config.Logger.Warn("retention notice rejected for good", "workspace_id", st.WorkspaceID, "slug", st.Slug)
		default:
			return ActionNone, fmt.Errorf("send notice: %w", err)
		}
	}
	if delivered == 0 {
		s.config.Logger.Warn("retention notice reached no admin; the workspace will not be deleted", "workspace_id", st.WorkspaceID, "slug", st.Slug, "admins", len(admins))
		return ActionNone, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE workspaces SET retention_notice_sent_at=$2 WHERE id=$1`, st.WorkspaceID, now); err != nil {
		return ActionNone, err
	}
	s.config.Logger.Info("retention notice sent", "workspace_id", st.WorkspaceID, "slug", st.Slug, "delivered", delivered, "admins", len(admins),
		"delete_on", earliestDeletion(st.LockedSince, now).Format(dateLayout))
	return ActionNotice, nil
}

func (s *Service) compose(st state, to string, now time.Time) ([]byte, error) {
	domainPart := s.config.From[strings.LastIndex(s.config.From, "@")+1:]
	id, err := email.NewMessageID(domainPart)
	if err != nil {
		return nil, err
	}
	date := earliestDeletion(st.LockedSince, now).Format(dateLayout)
	body := fmt.Sprintf(`The CollieDesk workspace "%s" has been locked since %s: its trial or subscription ended.

CollieDesk deletes a workspace that stays locked for 12 months. Yours will be deleted on %s (UTC), with all its conversations, contacts and attachments. Nothing can be recovered after that.

To keep it, choose a plan in Settings > Billing: %s
To keep a copy of your data, export it from the same screen before %s.

You get this message because you are an admin of the workspace.
`, st.Name, st.LockedSince.Format(dateLayout), date, s.config.Origin, date)
	return email.Compose(email.Outgoing{FromName: "CollieDesk", FromAddress: s.config.From, ToAddress: to,
		Subject: fmt.Sprintf("Your CollieDesk workspace %s will be deleted on %s", st.Name, date), Body: body,
		MessageID: id, Date: now, Standalone: true})
}
