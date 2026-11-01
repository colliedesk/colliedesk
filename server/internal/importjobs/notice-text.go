package importjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

func (w *Worker) composeNotice(ctx context.Context, n notice) ([]byte, error) {
	from := w.options.MailFrom
	origin := workspacehost.Origin(w.options.Origin, w.options.HelpCenterDomain, n.slug)
	progress, err := store.NewWithDB(w.options.DB).WithPlanLimits(w.options.Cloud).SetupProgress(ctx, n.workspaceID, w.options.Now().UTC())
	if err != nil {
		return nil, err
	}
	store.SetupLinks(&progress, helpcenter.URL(w.options.Origin, w.options.HelpCenterDomain, n.slug))
	subject, body := noticeText(n)
	body += "\nOpen the inbox: " + origin + "/\n"
	if center := progress.HelpCenter; center != nil {
		label := "View help center"
		if center.Action == "enable" {
			label = "Turn on the help center"
		}
		body += "\n" + label + ": " + absoluteNoticeURL(origin, center.URL) + "\n"
	}
	next := progress.Recommended
	if next == nil && progress.NextURL != nil {
		for _, step := range progress.Steps {
			if step.URL == *progress.NextURL {
				key := step.Key
				next = &key
				break
			}
		}
	}
	if next != nil && progress.NextURL != nil {
		labels := map[string]string{"import": "Import from Freshdesk", "custom_domain": "Verify your domain by TXT", "plan": "Choose a plan", "cname": "Change the CNAME", "search_console": "Keep Search Console verification and submit the sitemap",
			"email": "Receive customer email", "invite": "Invite your team", "first_reply": "Answer your first email"}
		label := labels[*next]
		for _, step := range progress.Steps {
			if step.Key == *next && step.Detail != nil && *step.Detail == "enable" {
				label = "Turn on the help center"
			}
		}
		body += "\nNext step: " + label + "\n" + absoluteNoticeURL(origin, *progress.NextURL) + "\n"
	}
	body += "\nImport report and recovery: " + origin + "/settings/import-freshdesk\n\nYou get this email because you started this import.\n"
	// Stable across retries, including a process restart after the relay accepted the message.
	id := "<import-" + n.id + "@" + from[strings.LastIndex(from, "@")+1:] + ">"
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: n.to, Subject: subject, Body: body,
		MessageID: id, Date: w.options.Now().UTC().Truncate(time.Second), Standalone: true})
}

func absoluteNoticeURL(origin, target string) string {
	if strings.HasPrefix(target, "/") {
		return origin + target
	}
	return target
}

func noticeText(n notice) (string, string) {
	headline := map[string]string{"OK": "complete", "FAILED": "incomplete", "UNVERIFIED": "done, not verified"}[n.status]
	if n.state == StateFailed || headline == "" {
		headline = "stopped"
	}
	subject := fmt.Sprintf("Freshdesk import %s: %s", headline, n.domain)
	body := fmt.Sprintf("The import of %s into your Colliedesk workspace %q has ended: %s.\n", n.domain, n.workspace, headline)
	var report freshdesk.Report
	if len(n.report) > 0 && json.Unmarshal(n.report, &report) == nil {
		body += fmt.Sprintf("\n%d of %d tickets, %d of %d contacts and %d of %d articles imported.\n", report.Tickets.Imported, report.Tickets.Source,
			report.Contacts.Imported, report.Contacts.Source, report.KnowledgeBase.Articles.Imported, report.KnowledgeBase.Articles.Source)
		if links := report.Links; links != nil {
			body += fmt.Sprintf("%d of %d old help-center addresses redirect to their new page at the time of the report.\n", links.OldAddresses.Redirected, links.OldAddresses.Found)
			for _, detail := range links.Detail {
				body += detail + "\n"
			}
		}
		for _, missing := range report.Missing {
			body += "Not imported: " + missing + "\n"
		}
		for _, failed := range report.Failures {
			body += fmt.Sprintf("Not imported: %s %s.\n", failed.Entity, failed.FreshdeskID)
		}
		for _, left := range report.NotImported {
			body += "Not imported: " + left + "\n"
		}
	}
	switch {
	case n.state == StateFailed:
		next := noticeMessages[n.code]
		if next == "" {
			next = "The import stopped on an error. Open the import report for the details and retry."
		}
		body += "\nThe full account has not been reconciled. What was imported stays. " + next + "\n"
	case n.status == freshdesk.StatusFailed:
		body += "\nSome items did not come over. Review the missing items in the report, then start a new import to retry them. Nothing is copied twice.\n"
	case n.status == freshdesk.StatusUnverified:
		body += "\nFreshdesk's totals could not confirm completeness. Check the unverified totals in the report before closing Freshdesk.\n"
	}
	return subject, body
}
