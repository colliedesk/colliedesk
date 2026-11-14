package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
)

// sendPortalLink emails one sign-in link. sent is false when this server has nowhere to send from.
func (a *API) sendPortalLink(ctx context.Context, workspaceID, helpTitle, to, link string) (bool, error) {
	mailer := a.config.PortalMailer
	if mailer == nil && a.config.Email.SMTP.Configured() {
		mailer = email.SMTPSender{Config: a.config.Email.SMTP}
	}
	from, name, ok := a.portalFrom(ctx, workspaceID, helpTitle)
	if mailer == nil || !ok {
		return false, nil
	}
	messageID, err := email.NewMessageID(strings.SplitN(from, "@", 2)[1])
	if err != nil {
		return true, err
	}
	subject := "Sign in to see your requests"
	body := "Use this link to see your requests"
	if helpTitle != "" {
		subject = "Sign in to " + helpTitle
		body += " at " + helpTitle
	}
	body += ". It works once and expires in 15 minutes.\n\n" + link + "\n\nIf you did not ask for this, you can ignore this email.\n"
	message, err := email.Compose(email.Outgoing{FromName: name, FromAddress: from, ToAddress: to, Subject: subject, Body: body,
		MessageID: messageID, Date: a.now(), Standalone: true})
	if err != nil {
		return true, err
	}
	if err := mailer.Send(ctx, from, []string{to}, message); err != nil {
		return true, err
	}
	return true, nil
}

// portalFrom is the address a portal sign-in link is sent from: noreply at the platform mail domain,
// or the workspace's support address when a self-hosted server has no platform domain.
func (a *API) portalFrom(ctx context.Context, workspaceID, title string) (address, name string, ok bool) {
	name = title
	if strings.TrimSpace(name) == "" {
		name = "Support"
	}
	if domain := a.config.Email.MailFromDomain; domain != "" {
		return "noreply@" + domain, name, email.ValidAddress("noreply@" + domain)
	}
	channel, err := a.store.GetEmailChannel(ctx, workspaceID)
	if err != nil || channel == nil || !email.ValidAddress(channel.SupportAddress) {
		return "", "", false
	}
	return channel.SupportAddress, name, true
}

// allowPortalLink bounds sign-in emails: five from one client address in 15 minutes, and five to one
// address in an hour. The counts live in this process, like the web form's.
func (a *API) allowPortalLink(request *http.Request, workspaceID, emailAddress string) (bool, time.Duration) {
	now := a.now()
	if allowed, wait := a.portalLinkByIP.allow(clientAddress(request, a.config.TrustedProxies), now); !allowed {
		return false, wait
	}
	return a.portalLinkByEmail.allow(workspaceID+"\n"+emailAddress, now)
}
