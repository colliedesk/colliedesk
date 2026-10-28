package freshdesk

import (
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"strconv"
	"strings"
	"time"
)

// Freshdesk ticket source "Outbound Email": the description was written by an agent.
const sourceOutboundEmail = 10

const emptyBody = "_(This message has no text. It was imported from Freshdesk.)_"

// mapStatus maps Freshdesk's built-in statuses; custom statuses become pending.
func mapStatus(status int) (string, bool) {
	switch status {
	case 2:
		return "open", true
	case 3:
		return "pending", true
	case 4:
		return "resolved", true
	case 5:
		return "closed", true
	default:
		return "pending", false
	}
}

func mapPriority(priority int) (string, bool) {
	switch priority {
	case 1:
		return "low", true
	case 2:
		return "normal", true
	case 3:
		return "high", true
	case 4:
		return "urgent", true
	default:
		return "normal", false
	}
}

// parseTimeSpent reads Freshdesk's "hh:mm" duration into minutes.
func parseTimeSpent(value string) (int, error) {
	hours, minutes, ok := strings.Cut(strings.TrimSpace(value), ":")
	h, errH := strconv.Atoi(hours)
	m, errM := strconv.Atoi(minutes)
	if !ok || errH != nil || errM != nil || h < 0 || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid time_spent %q", value)
	}
	return h*60 + m, nil
}

type newMessage struct {
	origin        *domain.MessageOrigin
	kind          string
	body          string
	html          string
	authorAgentID *string
	authorName    string
	createdAt     time.Time
}

func messageBody(text string) string {
	if strings.TrimSpace(text) == "" {
		return emptyBody
	}
	return text
}

func (r *run) descriptionMessage(ticket fdTicket) newMessage {
	kind, author := "incoming", ticket.RequesterID
	if ticket.Source == sourceOutboundEmail && ticket.ResponderID != nil {
		kind, author = "outgoing", *ticket.ResponderID
	}
	agentID, name := r.author(author)
	origin := &domain.MessageOrigin{System: "freshdesk", ExternalID: strconv.FormatInt(ticket.ID, 10), AuthorExternalID: r.agentExternalID(author),
		Kind: "description", To: list(ticket.ToEmails), CC: list(ticket.CcEmails), BCC: []string{}}
	return newMessage{origin: origin, kind: kind, body: messageBody(ticket.DescriptionText), html: ticket.Description,
		authorAgentID: agentID, authorName: name, createdAt: ticket.CreatedAt.UTC()}
}

// Freshdesk conversation sources (API v2 docs): 0 reply, 2 note, 8 forwarded email.
const (
	conversationNote      = 2
	conversationForwarded = 8
)

// conversationMessage maps a Freshdesk conversation to a timeline message.
// Only replies to the customer become outgoing: a forward went to a third
// party and a public note was not an email, so both become notes that keep
// their Freshdesk kind and recipients in origin.
func (r *run) conversationMessage(c fdConversation) newMessage {
	kind, originKind := "outgoing", "reply"
	switch {
	case c.Private:
		kind, originKind = "note", "private_note"
	case c.Source == conversationForwarded && c.Incoming:
		kind, originKind = "incoming", "forwarded_email"
	case c.Source == conversationForwarded:
		kind, originKind = "note", "forward"
	case c.Source == conversationNote:
		kind, originKind = "note", "public_note"
	case c.Incoming && c.AutoResponse:
		kind, originKind = "incoming", "automated_response"
	case c.Incoming:
		kind, originKind = "incoming", "incoming"
	}
	agentID, name := r.author(c.UserID)
	origin := &domain.MessageOrigin{System: "freshdesk", ExternalID: strconv.FormatInt(c.ID, 10), AuthorExternalID: r.agentExternalID(c.UserID),
		Kind: originKind, From: c.FromEmail, To: list(c.ToEmails), CC: list(c.CcEmails), BCC: list(c.BccEmails)}
	return newMessage{origin: origin, kind: kind, body: messageBody(c.BodyText), html: c.Body,
		authorAgentID: agentID, authorName: name, createdAt: c.CreatedAt.UTC()}
}

var ticketSources = map[int]string{1: "email", 2: "portal", 3: "phone", 7: "chat", 9: "feedback_widget", 10: "outbound_email"}

func ticketOrigin(ticket fdTicket, mailboxes []string) *domain.ConversationOrigin {
	source := "other"
	if name, ok := ticketSources[ticket.Source]; ok {
		source = name
	}
	var responder *string
	if ticket.ResponderID != nil {
		id := strconv.FormatInt(*ticket.ResponderID, 10)
		responder = &id
	}
	return &domain.ConversationOrigin{System: "freshdesk", ExternalID: strconv.FormatInt(ticket.ID, 10), ResponderExternalID: responder, Type: ticket.Type,
		Source: &source, DueAt: utcPointer(ticket.DueBy), FirstResponseDueAt: utcPointer(ticket.FrDueBy),
		CC: list(ticket.CcEmails), ForwardedTo: list(ticket.FwdEmails), Stats: originStats(ticket.Stats, ticket.Status), Mailboxes: mailboxes}
}

func originStats(stats *fdStats, freshdeskStatus int) *domain.OriginStats {
	if stats == nil {
		return nil
	}
	status, _ := mapStatus(freshdeskStatus)
	return &domain.OriginStats{Status: status, ResolvedAt: utcPointer(stats.ResolvedAt), FirstRespondedAt: utcPointer(stats.FirstRespondedAt), ClosedAt: utcPointer(stats.ClosedAt)}
}

func list(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// author resolves a Freshdesk user id to a Colliedesk agent (when matched by
// email) and a display name that is always kept.
// agentExternalID is the Freshdesk id of userID when it is an agent of the account, nil otherwise.
func (r *run) agentExternalID(userID int64) *string {
	if _, ok := r.agents[userID]; !ok {
		return nil
	}
	id := strconv.FormatInt(userID, 10)
	return &id
}

func (r *run) author(userID int64) (*string, string) {
	if agent, ok := r.agents[userID]; ok {
		if id, matched := r.agentTargets[userID]; matched {
			return &id, agent.Contact.Name
		}
		return nil, agent.Contact.Name
	}
	if name, ok := r.contactNames[userID]; ok && name != "" {
		return nil, name
	}
	return nil, "Freshdesk user " + strconv.FormatInt(userID, 10)
}
