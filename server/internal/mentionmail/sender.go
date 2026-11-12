// Package mentionmail emails each teammate a private note mentions, once (docs/user/mentions.md).
package mentionmail

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

// Sender emails the mentions that wait for their email, from the platform address.
type Sender struct {
	Store  *store.Store
	Mailer Mailer
	// From is the platform sender address (noreply@MAIL_FROM_DOMAIN); Origin and HelpCenterDomain make
	// the link to the note.
	From, Origin, HelpCenterDomain string
	Logger                         *slog.Logger
}

// AbandonedAfter is how long a claim may stay sending before another round takes it again: longer than
// any send to the relay can take (email.SMTPTimeout), so a slow send is never sent twice.
const AbandonedAfter = 10 * time.Minute

// RunOnce sends what waits and returns how many emails went.
func (s Sender) RunOnce(ctx context.Context) (int, error) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	due, err := s.Store.DueMentionEmails(ctx, AbandonedAfter, 200)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, item := range due {
		mail, claimed, err := s.Store.ClaimMentionEmail(ctx, item, AbandonedAfter)
		if err != nil {
			return sent, err
		}
		if !claimed {
			continue
		}
		status := s.send(ctx, mail, logger)
		if status == "sent" {
			sent++
		}
		if err := s.Store.SettleMentionEmail(context.WithoutCancel(ctx), item, status); err != nil {
			logger.Error("mention email not settled", "workspace_id", item.WorkspaceID, "error", err)
		}
	}
	return sent, nil
}

// send emails one mention: sent, rejected for good, or "" when it failed for now (it goes again later).
func (s Sender) send(ctx context.Context, mail store.MentionEmail, logger *slog.Logger) string {
	id, err := email.NewMessageID(s.From[strings.LastIndex(s.From, "@")+1:])
	if err != nil {
		logger.Error("mention email cannot be composed", "workspace_id", mail.WorkspaceID, "error", err)
		return "rejected"
	}
	link := workspacehost.Origin(s.Origin, s.HelpCenterDomain, mail.Slug) + "/conversations/" + mail.ConversationID + "#message-" + mail.MessageID
	subject, body := Text(mail, link)
	message, err := email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: s.From, ToAddress: mail.To, Subject: subject, Body: body,
		MessageID: id, Date: time.Now().UTC().Truncate(time.Second), Standalone: true})
	if err != nil {
		logger.Error("mention email cannot be composed", "workspace_id", mail.WorkspaceID, "error", err)
		return "rejected"
	}
	switch err := s.Mailer.Send(ctx, s.From, []string{mail.To}, message); {
	case err == nil:
		return "sent"
	case email.IsPermanent(err):
		logger.Warn("mention email rejected for good", "workspace_id", mail.WorkspaceID)
		return "rejected"
	default:
		logger.Error("mention email not sent, will retry", "workspace_id", mail.WorkspaceID, "error", err)
		return ""
	}
}

// Text is the subject and body of a mention email: who asked, on which conversation, the note's start
// and the link to it.
func Text(mail store.MentionEmail, link string) (string, string) {
	asker := mail.Asker
	if mail.Token != "" {
		asker += " via " + mail.Token
	}
	conversation := strings.TrimSpace(mail.DisplayRef + " " + mail.Subject)
	return fmt.Sprintf("%s mentioned you: %s", asker, mail.Subject),
		fmt.Sprintf("%s asked you in a private note on %s in %s:\n\n\"%s\"\n\nOpen the note: %s\n\n"+
			"It is in your Mentions list too. Turn these emails off under Account, Preferences: Email me when someone mentions me.\n",
			asker, conversation, mail.WorkspaceName, mail.Excerpt, link)
}

// Run sends what waits every interval until the context ends.
func (s Sender) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Default().Error("mention emails", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
