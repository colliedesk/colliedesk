// Package mailboxquiet notices an email channel that usually gets mail every day and has had none for a
// day, and tells the workspace's admins once (docs/user/email-channel.md "When no email arrives").
package mailboxquiet

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Worker records quiet spells, which the channel's status shows, and emails the admins of each spell
// once from the platform address. Without a Mailer it only records them.
type Worker struct {
	Store  *store.Store
	Mailer Mailer
	// From is the platform sender address (noreply@MAIL_FROM_DOMAIN); Origin and HelpCenterDomain make the
	// link to the channel; HostedMailDomain names the forwarding address to check.
	From, Origin, HelpCenterDomain, HostedMailDomain string
	Now                                              func() time.Time
	Logger                                           *slog.Logger
}

// RunOnce records what is quiet now and returns how many emails went out. A channel that fails logs it
// and the pass goes on with the rest.
func (w Worker) RunOnce(ctx context.Context) (int, error) {
	now, logger := time.Now().UTC(), w.Logger
	if w.Now != nil {
		now = w.Now()
	}
	if logger == nil {
		logger = slog.Default()
	}
	candidates, err := w.Store.QuietCandidates(ctx, now)
	if err != nil {
		return 0, err
	}
	mailing := w.Mailer != nil && w.From != ""
	sent := 0
	for _, candidate := range candidates {
		spell, err := w.Store.OpenQuietSpell(ctx, candidate, now, mailing)
		if err != nil {
			logger.Error("quiet mailbox", "workspace_id", candidate.WorkspaceID, "channel_id", candidate.ChannelID, "error", err)
			continue
		}
		if spell == nil {
			continue
		}
		for _, to := range spell.Recipients {
			status := w.send(ctx, *spell, to.Email, now, logger)
			if status == "sent" {
				sent++
			}
			if err := w.Store.SettleQuietEmail(context.WithoutCancel(ctx), *spell, to.AgentID, status, now); err != nil {
				logger.Error("quiet mailbox email not settled", "workspace_id", spell.WorkspaceID, "error", err)
			}
		}
	}
	return sent, nil
}

// Run records and tells every interval until the context ends.
func (w Worker) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Default().Error("quiet mailboxes", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// send emails one admin: sent, rejected for good, or "" when it failed for now (it goes again in a later
// pass of the same spell).
func (w Worker) send(ctx context.Context, spell store.QuietSpell, to string, now time.Time, logger *slog.Logger) string {
	message, err := w.compose(spell, to, now)
	if err != nil {
		logger.Error("quiet mailbox email cannot be composed", "workspace_id", spell.WorkspaceID, "error", err)
		return "rejected"
	}
	switch err := w.Mailer.Send(ctx, w.From, []string{to}, message); {
	case err == nil:
		return "sent"
	case email.IsPermanent(err):
		logger.Warn("quiet mailbox email rejected for good", "workspace_id", spell.WorkspaceID)
		return "rejected"
	default:
		logger.Error("quiet mailbox email not sent, will retry", "workspace_id", spell.WorkspaceID, "error", err)
		return ""
	}
}

func (w Worker) compose(spell store.QuietSpell, to string, now time.Time) ([]byte, error) {
	id, err := email.NewMessageID(w.From[strings.LastIndex(w.From, "@")+1:])
	if err != nil {
		return nil, err
	}
	subject, body := text(spell, w.HostedMailDomain, workspacehost.Origin(w.Origin, w.HelpCenterDomain, spell.Slug)+"/settings/channels/"+spell.ChannelID)
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: w.From, ToAddress: to, Subject: subject, Body: body,
		MessageID: id, Date: now.Truncate(time.Second), Standalone: true})
}

// text is the subject and body of the admins' email: the same sentence as the channel's warning, why it
// matters, and the link to the channel.
func text(spell store.QuietSpell, hostedMailDomain, link string) (string, string) {
	forwarding := email.HostedLocalAddress(spell.HostedLocal, spell.Slug, hostedMailDomain)
	warning := channelemail.QuietMessage(spell.SupportAddress, forwarding, spell.Since, spell.Timezone)
	location, err := time.LoadLocation(spell.Timezone)
	if err != nil {
		location = time.UTC
	}
	subject := "No email has reached " + spell.SupportAddress + " since " + spell.Since.In(location).Format("Mon 2 Jan 15:04 MST")
	return subject, warning + "\n\n" +
		"This address received email on most of the last seven days, so a whole day without any usually means mail no longer reaches Colliedesk: " +
		"the forwarding rule was removed, the domain's MX records changed, or the provider stopped forwarding. Customers who write meanwhile get no answer.\n\n" +
		"Open the channel:\n" + link + "\n\n" +
		"You get this as someone who can change the channels of " + spell.WorkspaceName + ": one email each time the address goes quiet.\n"
}
