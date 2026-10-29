package results

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5"
)

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Mail is how the worker sends the weekly email: from the platform address, linking to origin. Locked
// says whether a workspace's plan ended (hosted cloud only); a locked workspace gets no email.
type Mail struct {
	Mailer Mailer
	From   string
	Origin string
	// HelpCenterDomain gives each workspace its own host; the email links there.
	HelpCenterDomain string
	Locked           func(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (bool, error)
}

// WithMail makes the worker send the weekly email; without it (no outgoing mail settings) none is sent.
func (w *Worker) WithMail(mail Mail) *Worker {
	w.mail = &mail
	return w
}

type weeklyCandidate struct {
	workspaceID, timezone string
	sent                  *time.Time
}

// sendWeekly sends each due workspace's email once: Monday 8:00 in its timezone. The week is claimed
// (results_state.weekly_sent_week) before anything is sent, so two servers never send it twice; a
// failed send is logged, not retried. A workspace seen for the first time starts with the next week,
// never mid-week.
func (w *Worker) sendWeekly(ctx context.Context, now time.Time) error {
	if w.mail == nil || w.mail.Mailer == nil || w.mail.From == "" {
		return nil
	}
	var candidates []weeklyCandidate
	err := w.db.Privileged(ctx, database.PrivilegeResultsClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT s.workspace_id::text,w.timezone,s.weekly_sent_week FROM results_state s JOIN workspaces w ON w.id=s.workspace_id`)
		if err != nil {
			return fmt.Errorf("list weekly emails: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var candidate weeklyCandidate
			if err := rows.Scan(&candidate.workspaceID, &candidate.timezone, &candidate.sent); err != nil {
				return fmt.Errorf("list weekly emails: %w", err)
			}
			candidates = append(candidates, candidate)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		start := WeekStart(now, candidate.timezone)
		switch {
		case candidate.sent == nil:
			err = w.claimWeek(ctx, candidate.workspaceID, start, true)
		case !candidate.sent.Before(start) || now.Before(weeklyAt(start, candidate.timezone)):
			continue
		default:
			err = w.sendWorkspaceWeekly(ctx, candidate, start, now)
		}
		if err != nil {
			slog.Error("weekly results email", "workspace_id", candidate.workspaceID, "error", err)
		}
	}
	return nil
}

// claimWeekIn records start as the week whose email was handled; first only starts a workspace seen
// for the first time. It reports whether this call made the claim.
func (w *Worker) claimWeekIn(ctx context.Context, tx pgx.Tx, workspaceID string, start time.Time, first bool) (bool, error) {
	condition := `weekly_sent_week<$2`
	if first {
		condition = `weekly_sent_week IS NULL`
	}
	tag, err := tx.Exec(ctx, `UPDATE results_state SET weekly_sent_week=$2 WHERE workspace_id=$1 AND `+condition, workspaceID, start)
	if err != nil {
		return false, fmt.Errorf("claim the weekly email: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (w *Worker) claimWeek(ctx context.Context, workspaceID string, start time.Time, first bool) error {
	return w.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := w.claimWeekIn(ctx, tx, workspaceID, start, first)
		return err
	})
}

type weeklyContent struct {
	workspace, slug string
	page            Page
	to              []string
}

func (w *Worker) sendWorkspaceWeekly(ctx context.Context, candidate weeklyCandidate, start, now time.Time) error {
	var content *weeklyContent
	err := w.db.InWorkspace(ctx, candidate.workspaceID, func(tx pgx.Tx) error {
		claimed, err := w.claimWeekIn(ctx, tx, candidate.workspaceID, start, false)
		if err != nil || !claimed {
			return err
		}
		if w.mail.Locked != nil {
			locked, err := w.mail.Locked(ctx, tx, candidate.workspaceID, now)
			if err != nil || locked {
				return err
			}
		}
		page, err := Read(ctx, tx, candidate.workspaceID, Viewer{Now: now})
		if err != nil {
			return err
		}
		// Nothing to report yet: no numbers, or neither a baseline nor a conversation.
		if page.Current == nil || (page.BaselineState == "none" && page.Current.Windows["30d"].Volume.Count == 0) {
			return nil
		}
		content = &weeklyContent{page: page}
		if err := tx.QueryRow(ctx, `SELECT name, slug FROM workspaces WHERE id=$1`, candidate.workspaceID).Scan(&content.workspace, &content.slug); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT lower(email) FROM agents WHERE workspace_id=$1 AND role='admin' AND deactivated_at IS NULL AND results_email ORDER BY 1`, candidate.workspaceID)
		if err != nil {
			return err
		}
		content.to, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil || content == nil {
		return err
	}
	for _, address := range content.to {
		message, err := w.composeWeekly(content, candidate.timezone, address, now)
		if err == nil {
			err = w.mail.Mailer.Send(ctx, w.mail.From, []string{address}, message)
		}
		if err != nil {
			slog.Error("weekly results email", "workspace_id", candidate.workspaceID, "error", err)
		}
	}
	return nil
}

func (w *Worker) composeWeekly(content *weeklyContent, timezone, to string, now time.Time) ([]byte, error) {
	from := w.mail.From
	id, err := email.NewMessageID(from[strings.LastIndex(from, "@")+1:])
	if err != nil {
		return nil, err
	}
	origin := workspacehost.Origin(w.mail.Origin, w.mail.HelpCenterDomain, content.slug)
	subject, body := WeeklyText(content.workspace, content.page, timezone, origin, now)
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: to, Subject: subject, Body: body,
		MessageID: id, Date: now.Truncate(time.Second), Standalone: true, Unsubscribe: origin + "/results#weekly-email"})
}

// WeeklyText is the weekly email: the last 30 days next to the baseline's, last week's volume and the AI
// drafts. Totals only: no customer names, subjects or message text.
func WeeklyText(workspace string, page Page, timezone, origin string, now time.Time) (string, string) {
	loc := location(timezone)
	day := func(at time.Time) string { return at.In(loc).Format("Jan 2") }
	span := func(r Range) string { return day(r.From) + " – " + day(r.To) }
	current := page.Current.Windows["30d"]
	var before *Numbers
	source := "Freshdesk"
	if page.Baseline != nil && page.BaselineState == "ready" && page.Baseline.ImportStatus != "FAILED" {
		before = page.Baseline.Windows["30d"]
		if strings.HasPrefix(page.Baseline.Source, "helpscout:") {
			source = "Help Scout"
		}
	}
	duration := func(seconds *int64) string {
		if seconds == nil {
			return "too few to tell"
		}
		return formatDuration(*seconds)
	}
	subject := fmt.Sprintf("%s weekly results: first reply %s", workspace, duration(current.FirstResponse.MedianSeconds))
	if before != nil && before.FirstResponse.MedianSeconds != nil {
		subject += fmt.Sprintf(" (%s: %s)", source, formatDuration(*before.FirstResponse.MedianSeconds))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Weekly results for %s\n%s. Dates in workspace time (%s).\n\n", workspace, now.In(loc).Format("Monday, Jan 2, 2006"), timezone)
	if before != nil {
		fmt.Fprintf(&b, "Last 30 days, next to %s (median unless it says otherwise)\n\n", source)
		if page.Baseline.ImportStatus == "UNVERIFIED" {
			fmt.Fprintf(&b, "The %s numbers come from an UNVERIFIED import: it could not check that every ticket arrived. The import report says why.\n\n", source)
		}
	} else {
		b.WriteString("Last 30 days (median unless it says otherwise)\n\n")
	}
	row := func(name, now, then string) {
		if before != nil {
			fmt.Fprintf(&b, "- %s: %s (%s: %s)\n", name, now, source, then)
		} else {
			fmt.Fprintf(&b, "- %s: %s\n", name, now)
		}
	}
	var thenFirst, thenResolve, thenWeek string
	if before != nil {
		thenFirst = duration(before.FirstResponse.MedianSeconds)
		thenResolve = "not available"
		if before.Resolution != nil {
			thenResolve = duration(before.Resolution.MedianSeconds)
		}
		thenWeek = "too few to tell"
		if before.Volume.PerWeek != nil {
			thenWeek = fmt.Sprintf("%.0f a week", *before.Volume.PerWeek)
		}
	}
	row("First response time", duration(current.FirstResponse.MedianSeconds), thenFirst)
	switch {
	case current.AvailableOn != nil:
		row("Time to resolve", "available on "+day(*current.AvailableOn), thenResolve)
	case current.Resolution != nil:
		row("Time to resolve", duration(current.Resolution.MedianSeconds), thenResolve)
	}
	if current.Reopened != nil && current.Reopened.Rate != nil {
		fmt.Fprintf(&b, "- Reopened within 7 days: %.0f%% (%d of %d resolutions)\n", *current.Reopened.Rate*100, current.Reopened.Reopened, current.Reopened.Resolutions)
	}
	week := "too few to tell"
	if current.Volume.PerWeek != nil {
		week = fmt.Sprintf("%.0f a week", *current.Volume.PerWeek)
	}
	row("Conversations per week", week, thenWeek)
	fmt.Fprintf(&b, "\nColliedesk: conversations created %s; time to resolve and reopens, created %s.\n", span(current.Created), span(current.ResolvedCreated))
	if before != nil {
		fmt.Fprintf(&b, "%s: created %s; time to resolve, created %s.\n", source, span(before.Created), span(before.ResolvedCreated))
	}
	if lastWeek := page.Current.Windows["7d"]; lastWeek != nil {
		fmt.Fprintf(&b, "\nLast week: %d conversations came in, %s.", lastWeek.Volume.Count, span(lastWeek.Created))
		if before != nil && before.Volume.PerWeek != nil {
			fmt.Fprintf(&b, " In %s your team had %.0f a week.", source, *before.Volume.PerWeek)
		}
		b.WriteString("\n")
	}
	if ai := current.AI; ai != nil && ai.SentUnchanged+ai.SentEdited+ai.Discarded > 0 {
		fmt.Fprintf(&b, "\nAI drafts, last 30 days: %d sent nearly unchanged, %d sent after edits, %d discarded.\n", ai.SentUnchanged, ai.SentEdited, ai.Discarded)
		if saved := page.HoursSaved["30d"]; saved != nil {
			fmt.Fprintf(&b, "Up to %s h saved: %d conversations with a draft sent nearly unchanged × %d min, the median handle time in %s.\n",
				strings.TrimSuffix(fmt.Sprintf("%.1f", saved.Hours), ".0"), saved.Conversations, saved.MedianHandleMinutes, source)
		}
	}
	fmt.Fprintf(&b, "\nEvery number, and the conversations behind it: %s/results\n", origin)
	fmt.Fprintf(&b, "\nYou get this email because you are an admin of %s and the weekly email is on. It holds totals only, never customer names or messages. Stop these emails: %s/results#weekly-email\n", workspace, origin)
	return subject, b.String()
}

// formatDuration writes seconds the way the page does: minutes, hours and minutes, days and hours.
func formatDuration(seconds int64) string {
	minutes := (seconds + 30) / 60
	switch {
	case minutes < 60:
		return fmt.Sprintf("%d min", minutes)
	case minutes < 24*60:
		if minutes%60 == 0 {
			return fmt.Sprintf("%d h", minutes/60)
		}
		return fmt.Sprintf("%d h %02d min", minutes/60, minutes%60)
	default:
		hours := (minutes + 30) / 60
		if hours%24 == 0 {
			return fmt.Sprintf("%d d", hours/24)
		}
		return fmt.Sprintf("%d d %d h", hours/24, hours%24)
	}
}
