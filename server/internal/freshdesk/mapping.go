package freshdesk

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Freshdesk ticket source "Outbound Email": the description was written by an agent.
const sourceOutboundEmail = 10

const emptyBody = "_(This message has no text. It was imported from Freshdesk.)_"

var inlineImage = regexp.MustCompile(`(?i)<img[^>]+src=["'][^"']*freshdesk`)

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
	return newMessage{kind: kind, body: messageBody(ticket.DescriptionText), html: ticket.Description,
		authorAgentID: agentID, authorName: name, createdAt: ticket.CreatedAt.UTC()}
}

func (r *run) conversationMessage(c fdConversation) newMessage {
	kind := "outgoing"
	switch {
	case c.Private:
		kind = "note"
	case c.Incoming:
		kind = "incoming"
	}
	agentID, name := r.author(c.UserID)
	return newMessage{kind: kind, body: messageBody(c.BodyText), html: c.Body,
		authorAgentID: agentID, authorName: name, createdAt: c.CreatedAt.UTC()}
}

// author resolves a Freshdesk user id to a CollieDesk agent (when matched by
// email) and a display name that is always kept.
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
