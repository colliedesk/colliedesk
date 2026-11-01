package importjobs

import (
	"context"
	"errors"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// noticeMessages say what an ended import means and what to do, by error code; the page says more.
var noticeMessages = map[string]string{
	CodeKeyRejected:      "Freshdesk refused the API key. Check it in Freshdesk (Profile settings > API key) and start a new import.",
	CodeNeedsAdminKey:    "The API key belongs to an agent without admin rights. Start a new import with the key of a Freshdesk admin.",
	CodePlanNoAPI:        "The Freshdesk account has no API calls: the Free plan has none, and a trial of a higher plan that ended falls back to it. Start a 14-day trial of a higher Freshdesk plan, then start a new import.",
	CodeUnavailable:      "Freshdesk could not be reached for a day. Start a new import later: nothing is copied twice.",
	CodeRunningElsewhere: "Another import or undo kept the workspace busy for a day. Start a new import once it ends.",
	CodeStorageCap:       "The rest of the account does not fit the attachment storage of the plan. Move to a plan with more storage, then start a new import.",
	CodeKnowledgeBase:    "The knowledge base is larger than a workspace can hold.",
	CodeNoProgress:       "The import stopped several times without getting further. Start a new import, or report a problem.",
}

// sendNotices resumes durable notices after a restart or relay failure.
func (w *Worker) sendNotices(ctx context.Context) {
	if w.options.Mailer == nil || w.options.MailFrom == "" {
		return
	}
	jobs, err := dueNotices(ctx, w.options.DB, w.options.Now().UTC())
	if err != nil {
		w.options.Logger.Error("import notices", "error", err)
		return
	}
	for _, job := range jobs {
		w.sendNotice(ctx, job)
	}
}

type notice struct {
	id, workspaceID, workspace, slug, domain, state, status, code, to string
	report                                                            []byte
}

// sendNotice locks one pending job until the relay answers and its result is committed. A competing
// worker skips it. A failed or interrupted delivery stays pending across restarts. Message-ID is stable
// across retries; SMTP cannot atomically commit with PostgreSQL, so a lost relay acknowledgement may retry.
func (w *Worker) sendNotice(ctx context.Context, item claimed) {
	if w.options.Mailer == nil || w.options.MailFrom == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := w.options.DB.InWorkspace(ctx, item.workspaceID, func(tx pgx.Tx) error {
		n := notice{id: item.id, workspaceID: item.workspaceID}
		err := tx.QueryRow(ctx, `SELECT w.name,w.slug,j.domain,j.state,COALESCE(j.status,''),COALESCE(j.error_code,''),
			COALESCE(a.email,''),COALESCE(j.report,j.interim_report)
			FROM import_jobs j JOIN workspaces w ON w.id=j.workspace_id
			LEFT JOIN agents a ON a.workspace_id=j.workspace_id AND a.id=j.created_by AND a.deactivated_at IS NULL
			WHERE j.workspace_id=$1 AND j.id=$2 AND j.kind='import' AND j.phase<>'preview' AND j.state IN ('finished','failed')
			AND j.notice_state='pending' AND j.notice_next_attempt_at<=$3 FOR UPDATE OF j SKIP LOCKED`, item.workspaceID, item.id, w.options.Now().UTC()).
			Scan(&n.workspace, &n.slug, &n.domain, &n.state, &n.status, &n.code, &n.to, &n.report)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		state := "sent"
		if n.to == "" {
			state = "rejected"
		} else {
			message, err := w.composeNotice(ctx, n)
			if err == nil {
				err = w.options.Mailer.Send(ctx, w.options.MailFrom, []string{n.to}, message)
			}
			if err != nil {
				state = "pending"
				if email.IsPermanent(err) {
					state = "rejected"
				}
				w.options.Logger.Error("import notice", "job", item.id, "error", err)
			}
		}
		_, err = tx.Exec(ctx, `UPDATE import_jobs SET notice_state=$3,notice_next_attempt_at=$4 WHERE workspace_id=$1 AND id=$2`,
			item.workspaceID, item.id, state, w.options.Now().UTC().Add(time.Minute))
		return err
	})
	if err != nil && ctx.Err() == nil {
		w.options.Logger.Error("import notice", "job", item.id, "error", err)
	}
}
