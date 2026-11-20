// Package kbreviewmail emails the people asked to review a knowledge base text, and its author once it is
// decided (docs/user/knowledge-base.md).
package kbreviewmail

import (
	"context"
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

// Sender emails the review requests and decisions that wait for their email, from the platform address.
type Sender struct {
	Store  *store.Store
	Mailer Mailer
	// From is the platform sender address (noreply@MAIL_FROM_DOMAIN); Origin and HelpCenterDomain make
	// the link to the review or the article.
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
	due, err := s.Store.DueKBReviewEmails(ctx, AbandonedAfter, 200)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, item := range due {
		mail, claimed, err := s.Store.ClaimKBReviewEmail(ctx, item, AbandonedAfter)
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
		if err := s.Store.SettleKBReviewEmail(context.WithoutCancel(ctx), item, status); err != nil {
			logger.Error("review email not settled", "workspace_id", item.WorkspaceID, "error", err)
		}
	}
	return sent, nil
}

// send emails one review email: sent, rejected for good, or "" when it failed for now (it goes again).
func (s Sender) send(ctx context.Context, mail store.KBReviewEmail, logger *slog.Logger) string {
	id, err := email.NewMessageID(s.From[strings.LastIndex(s.From, "@")+1:])
	if err != nil {
		logger.Error("review email cannot be composed", "workspace_id", mail.WorkspaceID, "error", err)
		return "rejected"
	}
	origin := workspacehost.Origin(s.Origin, s.HelpCenterDomain, mail.Slug)
	var subject, body string
	if mail.AgentID != "" {
		subject, body = RequestText(mail, origin+"/knowledge/reviews/"+mail.ReviewID)
	} else {
		subject, body = DecisionText(mail, origin+articlePath(mail))
	}
	message, err := email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: s.From, ToAddress: mail.To, Subject: subject, Body: body,
		MessageID: id, Date: time.Now().UTC().Truncate(time.Second), Standalone: true})
	if err != nil {
		logger.Error("review email cannot be composed", "workspace_id", mail.WorkspaceID, "error", err)
		return "rejected"
	}
	switch err := s.Mailer.Send(ctx, s.From, []string{mail.To}, message); {
	case err == nil:
		return "sent"
	case email.IsPermanent(err):
		logger.Warn("review email rejected for good", "workspace_id", mail.WorkspaceID)
		return "rejected"
	default:
		logger.Error("review email not sent, will retry", "workspace_id", mail.WorkspaceID, "error", err)
		return ""
	}
}

func articlePath(mail store.KBReviewEmail) string {
	path := "/knowledge/articles/" + mail.Review.ArticleID
	if mail.Review.Locale != nil {
		path += "/translations/" + *mail.Review.Locale
	}
	return path
}

// Run sends what waits every interval until the context ends.
func (s Sender) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Default().Error("review emails", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
