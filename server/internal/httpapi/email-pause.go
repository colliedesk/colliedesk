package httpapi

import (
	"context"
	"fmt"
	"log/slog"
)

// pauseOnComplaints pauses the workspace's sending when its complaints are over the limits and tells the
// operator, once: only the call that makes the pause gets it back from the store. A failed alert is
// logged and does not undo the pause; the settings page and the held replies show it either way.
func (a *API) pauseOnComplaints(ctx context.Context, workspaceID string) error {
	paused, err := a.store.PauseEmailOnComplaints(ctx, workspaceID, a.now())
	if err != nil || paused == nil {
		return err
	}
	slog.Warn("email sending paused after spam complaints", "workspace", workspaceID, "slug", paused.Slug, "reason", paused.Pause.Reason)
	if !a.operatorAlertReady() {
		slog.Warn("no operator alert is configured for the email pause (set BILLING_ALERT_EMAIL, SMTP_HOST and MAIL_FROM_DOMAIN)", "workspace", workspaceID, "slug", paused.Slug)
		return nil
	}
	body := fmt.Sprintf(`Email sending was paused for a workspace after spam complaints. Its replies stay queued and are not sent.

Workspace: %s (%s)
Reason: %s

Look at the replies that drew complaints, then lift the pause with:
  collie email resume %s
`, paused.Name, paused.Slug, paused.Pause.Reason, paused.Slug)
	if err := a.sendOperatorAlert(ctx, "Colliedesk: email sending paused for "+paused.Slug+" (spam complaints)", body); err != nil {
		slog.Error("send operator alert", "error", err, "workspace", workspaceID)
	}
	return nil
}
