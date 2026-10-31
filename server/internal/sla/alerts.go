package sla

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Alerts sends the reminder and overdue emails of the response targets, each once.
type Alerts struct {
	Store  *store.Store
	Mailer Mailer
	// From is the platform sender address (noreply@MAIL_FROM_DOMAIN); Origin and HelpCenterDomain make
	// the link to the conversation.
	From, Origin, HelpCenterDomain string
	Now                            func() time.Time
	Logger                         *slog.Logger
}

// RunOnce sends what is due and returns how many emails went.
func (a Alerts) RunOnce(ctx context.Context) (int, error) {
	now, logger := time.Now().UTC(), a.Logger
	if a.Now != nil {
		now = a.Now()
	}
	if logger == nil {
		logger = slog.Default()
	}
	due, err := a.Store.DueSLAAlerts(ctx, now, 200)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, item := range due {
		alert, claimed, err := a.Store.ClaimSLAAlert(ctx, item)
		if err != nil {
			return sent, err
		}
		if !claimed {
			continue
		}
		status, delivered := a.send(ctx, alert, now, logger)
		if err := a.Store.SettleSLAAlert(context.WithoutCancel(ctx), alert, status, delivered); err != nil {
			logger.Error("response alert not settled", "workspace_id", alert.WorkspaceID, "error", err)
		}
		sent += delivered
	}
	return sent, nil
}

// send emails one alert to each recipient; "" when a send failed for now (the alert goes again).
func (a Alerts) send(ctx context.Context, alert store.SLAAlert, now time.Time, logger *slog.Logger) (string, int) {
	if len(alert.To) == 0 {
		return "nobody", 0
	}
	delivered := 0
	for _, to := range alert.To {
		message, err := a.compose(alert, to, now)
		if err != nil {
			logger.Error("response alert cannot be composed", "workspace_id", alert.WorkspaceID, "error", err)
			return "rejected", delivered
		}
		switch err := a.Mailer.Send(ctx, a.From, []string{to}, message); {
		case err == nil:
			delivered++
		case email.IsPermanent(err):
			logger.Warn("response alert rejected for good", "workspace_id", alert.WorkspaceID)
		case delivered == 0:
			logger.Error("response alert not sent, will retry", "workspace_id", alert.WorkspaceID, "error", err)
			return "", 0
		}
	}
	if delivered == 0 {
		return "rejected", 0
	}
	return "sent", delivered
}

func (a Alerts) compose(alert store.SLAAlert, to string, now time.Time) ([]byte, error) {
	id, err := email.NewMessageID(a.From[strings.LastIndex(a.From, "@")+1:])
	if err != nil {
		return nil, err
	}
	subject, body := Text(alert, workspacehost.Origin(a.Origin, a.HelpCenterDomain, alert.Slug)+"/conversations/"+alert.ConversationID)
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: a.From, ToAddress: to, Subject: subject, Body: body,
		MessageID: id, Date: now.Truncate(time.Second), Standalone: true})
}

// Text is the subject and body of an alert, with the time it is due in the workspace's timezone.
func Text(alert store.SLAAlert, link string) (string, string) {
	what := map[string]string{"first_response": "Reply", "resolution": "Resolve"}[alert.Clock]
	location, err := time.LoadLocation(alert.Timezone)
	if err != nil {
		location = time.UTC
	}
	due := alert.DueAt.In(location).Format("Mon 2 Jan 15:04 MST")
	if alert.Threshold == "overdue" {
		return fmt.Sprintf("%s overdue: %s %s", what, alert.DisplayRef, alert.Subject),
			fmt.Sprintf("%s %s in %s is past its response target: it was due %s.\n\n%s\n\nYou get this as the person it is assigned to, or as an admin while nobody is. Turn these emails off under Settings, Response targets.\n",
				alert.DisplayRef, alert.Subject, alert.WorkspaceName, due, link)
	}
	return fmt.Sprintf("%s due soon: %s %s", what, alert.DisplayRef, alert.Subject),
		fmt.Sprintf("%s %s in %s is due %s.\n\n%s\n\nYou get this as the person it is assigned to, or as an admin while nobody is. Turn these emails off under Settings, Response targets.\n",
			alert.DisplayRef, alert.Subject, alert.WorkspaceName, due, link)
}

// Run sends what is due every interval until the context ends.
func (a Alerts) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := a.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Default().Error("response alerts", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
