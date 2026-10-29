package importjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
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

// notifyAdmins mails, one message each, the person who started an import and every active admin when it ends on its own
// (finished or failed), from the platform address. A preview (the person is on the page), a cancel (a
// person did it) and an undo send nothing. Without a mailer the page alone says it. It is sent once,
// after the end is committed; a failure to send is logged, never retried.
func (r *jobRun) notifyAdmins(ctx context.Context, state, status, code string, report []byte) {
	w := r.worker
	if w.options.Mailer == nil || w.options.MailFrom == "" || r.job.Kind != KindImport || r.job.Phase == PhasePreview ||
		(state != StateFinished && state != StateFailed) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	var name string
	var to []string
	err := w.options.DB.InWorkspace(ctx, r.item.workspaceID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT name FROM workspaces WHERE id=$1`, r.item.workspaceID).Scan(&name); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT lower(email) FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL
			AND (role='admin' OR id=(SELECT created_by FROM import_jobs WHERE workspace_id=$1 AND id=$2)) ORDER BY 1`, r.item.workspaceID, r.item.id)
		if err != nil {
			return err
		}
		to, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil || len(to) == 0 {
		if err != nil {
			w.options.Logger.Error("import notice", "job", r.item.id, "error", err)
		}
		return
	}
	for _, address := range to {
		message, err := r.composeNotice(name, address, state, status, code, report)
		if err == nil {
			err = w.options.Mailer.Send(ctx, w.options.MailFrom, []string{address}, message)
		}
		if err != nil {
			w.options.Logger.Error("import notice", "job", r.item.id, "error", err)
		}
	}
}

func (r *jobRun) composeNotice(workspace, to string, state, status, code string, report []byte) ([]byte, error) {
	from := r.worker.options.MailFrom
	id, err := email.NewMessageID(from[strings.LastIndex(from, "@")+1:])
	if err != nil {
		return nil, err
	}
	page := workspacehost.Origin(r.worker.options.Origin, r.worker.options.HelpCenterDomain, r.job.slug) + "/settings/import-freshdesk"
	var subject, body string
	if state == StateFinished {
		headline := map[string]string{"OK": "complete", "FAILED": "incomplete", "UNVERIFIED": "done, not verified"}[status]
		subject = fmt.Sprintf("Freshdesk import %s: %s", headline, r.job.Domain)
		var counts struct {
			Tickets struct {
				Source   int `json:"source"`
				Imported int `json:"imported"`
			} `json:"tickets"`
			Missing []string `json:"missing"`
		}
		_ = json.Unmarshal(report, &counts)
		explain := map[string]string{
			"OK":         "Every item Freshdesk holds is in Colliedesk, checked against Freshdesk's own totals.",
			"FAILED":     "Some items were not imported; the report lists them. Start a new import to try them again: nothing is copied twice.",
			"UNVERIFIED": "Nothing failed, but Freshdesk's totals could not confirm that everything was imported. The report says what to check.",
		}[status]
		body = fmt.Sprintf("The import of %s into the Colliedesk workspace \"%s\" has ended: %s.\n\n%d of %d tickets are in Colliedesk. %s\n\nThe report, to read or download, and the undo: %s\n",
			r.job.Domain, workspace, status, counts.Tickets.Imported, counts.Tickets.Source, explain, page)
	} else {
		subject = fmt.Sprintf("Freshdesk import stopped: %s", r.job.Domain)
		next := noticeMessages[code]
		if next == "" {
			next = "It stopped on an error; the import page shows the details."
		}
		body = fmt.Sprintf("The import of %s into the Colliedesk workspace \"%s\" stopped before it finished. What it imported stays.\n\n%s\n\nDetails, a new import or the undo: %s\n",
			r.job.Domain, workspace, next, page)
	}
	body += "\nYou get this message because you are an admin of the workspace or started the import.\n"
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: to, Subject: subject, Body: body,
		MessageID: id, Date: r.now().Truncate(time.Second), Standalone: true})
}
