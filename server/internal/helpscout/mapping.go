package helpscout

import (
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
)

const emptyBody = "_(This message has no text. It was imported from Help Scout.)_"

// mapStatus maps Help Scout's conversation statuses; anything else becomes pending and is reported.
func (r *run) mapStatus(status, key string) string {
	switch status {
	case "active":
		return "open"
	case "pending":
		return "pending"
	case "closed":
		return "closed"
	}
	r.warn("unknown_status_mapped", "Conversation has a Help Scout status the importer does not know; it was imported as pending.", key)
	return "pending"
}

// assignee returns the Colliedesk agent for the Help Scout assignee. keep leaves the assignee of an
// existing conversation alone: a deactivated agent keeps the work they did.
func (r *run) assignee(ref hsConversation, key string) (id *string, keep bool) {
	if ref.Assignee == nil || ref.Assignee.ID == 0 {
		return nil, false
	}
	if ref.Assignee.Type == "team" {
		r.warn("assigned_to_team", "Conversation is assigned to a Help Scout team; Colliedesk assigns people only, so it was imported unassigned.", key)
		return nil, false
	}
	target, matched := r.agentTargets[ref.Assignee.ID]
	switch {
	case !matched:
		return nil, false
	case r.deactivated[ref.Assignee.ID]:
		r.warn("assignee_deactivated", "Conversation's Help Scout assignee is a deactivated Colliedesk agent, so it was not assigned to them. Reactivate the agent and re-run to assign it.", key)
		return nil, true
	}
	return &target, false
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

// threadMessage maps a Help Scout thread to a timeline message. Only an agent's reply to the
// customer becomes outgoing; a note, a forward and a logged phone call become internal notes that
// keep their Help Scout kind in origin, so nothing private is ever shown as sent to the customer.
func (r *run) threadMessage(t hsThread) newMessage {
	kind, originKind := "note", "other"
	switch t.Type {
	case "customer":
		kind, originKind = "incoming", "incoming"
	case "message":
		kind, originKind = "outgoing", "reply"
	case "note":
		kind, originKind = "note", "private_note"
	case "forwardparent", "forwardchild":
		kind, originKind = "note", "forward"
	case "chat", "beaconchat":
		kind, originKind = "outgoing", "reply"
		if t.CreatedBy.Type == "customer" {
			kind, originKind = "incoming", "incoming"
		}
	}
	agentID, name := r.author(t.CreatedBy)
	var from *string
	if address := strings.TrimSpace(t.CreatedBy.Email); address != "" {
		from = &address
	}
	origin := &domain.MessageOrigin{System: "helpscout", ExternalID: strconv.FormatInt(t.ID, 10), Kind: originKind,
		From: from, To: list(t.To), CC: list(t.CC), BCC: list(t.BCC)}
	body := email.HTMLToText(t.Body)
	if body == "" {
		body = emptyBody
	}
	return newMessage{origin: origin, kind: kind, body: body, html: t.Body, authorAgentID: agentID, authorName: name, createdAt: t.CreatedAt.UTC()}
}

// author resolves the creator of a thread to a Colliedesk agent (users matched by email) and a
// display name that is always kept.
func (r *run) author(person hsPerson) (*string, string) {
	name := fullName(person.First, person.Last)
	if person.Type == "user" {
		if user, ok := r.users[person.ID]; ok && name == "" {
			name = fullName(user.FirstName, user.LastName)
		}
		if name == "" {
			name = "Help Scout user " + strconv.FormatInt(person.ID, 10)
		}
		if id, ok := r.agentTargets[person.ID]; ok {
			return &id, name
		}
		return nil, name
	}
	if name == "" {
		name = strings.TrimSpace(person.Email)
	}
	if name == "" {
		name = "Help Scout customer " + strconv.FormatInt(person.ID, 10)
	}
	return nil, name
}
