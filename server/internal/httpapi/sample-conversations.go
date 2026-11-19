package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
)

// sampleTagName marks the conversations a new workspace starts with, so a person and an AI agent can tell
// them from real mail and close them when done.
const sampleTagName = "sample"

type sampleConversation struct {
	subject string
	ask     []string
}

// sampleConversations are written as the questions a small support team gets in its first week. Each
// ends by saying what it is and where a reply goes.
var sampleConversations = []sampleConversation{
	{
		subject: "I was charged twice this month",
		ask: []string{
			"Hi,",
			"",
			"My card shows two charges for this month's subscription, both on the same day. Can you refund one of them?",
			"",
			"Thanks,",
		},
	},
	{
		subject: "The password reset email never arrives",
		ask: []string{
			"Hello,",
			"",
			"I asked for a password reset three times this morning and no email came, not even in spam. I can't sign in at all. What should I do?",
		},
	},
	{
		subject: "Can I export all our data?",
		ask: []string{
			"Hi there,",
			"",
			"Before we commit for the year, can we export everything we keep with you (customers, history, attachments)? Which format is it in?",
		},
	},
}

// addSampleConversations gives a workspace that signed up from an AI app a few conversations to work on at
// once. They arrive from the person's own address through the pipeline real mail takes, so a reply reaches
// only them, and the milestones do not count them as a customer writing in. A failure is logged and the
// sign-up goes on: the inbox then starts empty, as a web sign-up's does.
func (a *API) addSampleConversations(ctx context.Context, workspaceID string, agent domain.Agent) {
	added, err := a.deliverSampleConversations(ctx, workspaceID, agent)
	if err != nil {
		slog.Warn("sample conversations not added", "workspace_id", workspaceID, "added", added, "error", err)
	}
	if added > 0 {
		a.recordFunnel("sample_conversations_added", workspaceID, map[string]any{"count": added})
	}
}

func (a *API) deliverSampleConversations(ctx context.Context, workspaceID string, agent domain.Agent) (int, error) {
	if a.config.Attachments == nil {
		return 0, errors.New("attachment storage is not configured")
	}
	target, err := a.store.GetEmailChannel(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	if target == nil {
		return 0, errors.New("the workspace has no email channel")
	}
	if email.MailboxKey(agent.Email) == email.MailboxKey(target.SupportAddress) {
		return 0, errors.New("the person signs in with the support address")
	}
	channel, err := a.store.GetChannel(ctx, workspaceID, target.ChannelID)
	if err != nil {
		return 0, err
	}
	tag, err := a.store.CreateTag(ctx, workspaceID, sampleTagName, "#64748b")
	if err != nil {
		return 0, err
	}
	added := 0
	for _, sample := range sampleConversations {
		raw, err := selfMessage(agent, target.SupportAddress, sample.subject, sampleLines(sample, agent.Email), a.now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
		if err != nil {
			return added, err
		}
		parsed, err := email.Parse(raw, "")
		if err != nil {
			return added, fmt.Errorf("parse sample conversation: %w", err)
		}
		// The product wrote it for the person; it never passed the support mailbox.
		parsed.Authentication = email.AuthPass
		parsed.Direct = true
		results, err := a.pipeline.Receive(ctx, channel, []channels.Raw{{Parsed: parsed, Body: raw, ReceivedAt: a.now()}})
		if err != nil {
			return added, err
		}
		if results[0].ConversationID == nil {
			return added, fmt.Errorf("sample conversation was not stored (%s)", results[0].Status)
		}
		if _, err := a.store.AddConversationTag(ctx, workspaceID, *results[0].ConversationID, tag.ID); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func sampleLines(sample sampleConversation, address string) []string {
	return append(append([]string(nil), sample.ask...),
		"",
		"--",
		"This is a sample conversation Colliedesk added to your new inbox, so you and your AI app have something to try.",
		"A reply you send is emailed only to "+address+". Close the conversation when you are done with it.",
	)
}
