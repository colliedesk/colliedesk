package mcpserver

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The output types below are what a model reads. Text a customer wrote sits only in the
// UntrustedCustomerContent fields, so a model (and a reader of the transcript) can tell it from
// Colliedesk's own words.

type personOut struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type contactOut struct {
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	Organization *string `json:"organization,omitempty"`
}

type conversationContent struct {
	Subject string      `json:"subject"`
	Contact *contactOut `json:"contact"`
}

type conversationOut struct {
	ID                       string              `json:"id"`
	Status                   string              `json:"status"`
	Priority                 string              `json:"priority"`
	Assignee                 *personOut          `json:"assignee"`
	Tags                     []string            `json:"tags"`
	Channel                  string              `json:"channel,omitempty"`
	CreatedAt                string              `json:"created_at,omitempty"`
	UpdatedAt                string              `json:"updated_at"`
	ContactID                string              `json:"contact_id,omitempty"`
	UntrustedCustomerContent conversationContent `json:"untrusted_customer_content"`
}

// stateOut is a conversation without its customer-written parts, for tools that change one.
type stateOut struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Priority  string     `json:"priority"`
	Assignee  *personOut `json:"assignee"`
	Tags      []string   `json:"tags"`
	UpdatedAt string     `json:"updated_at"`
}

// utc formats an instant as UTC to the second, the form a model can compare and convert.
func utc(moment time.Time) string { return moment.UTC().Format("2006-01-02T15:04:05Z") }

func agentName(agent restAgent) string {
	if name := strings.TrimSpace(agent.DisplayName); name != "" {
		return name
	}
	return agent.Email
}

func person(agents map[string]restAgent, id *string) *personOut {
	if id == nil {
		return nil
	}
	agent, ok := agents[*id]
	if !ok {
		return &personOut{ID: *id}
	}
	return &personOut{ID: agent.ID, Name: agentName(agent), Email: agent.Email}
}

func tagNames(tags []restTag) []string {
	names := make([]string, len(tags))
	for index, tag := range tags {
		names[index] = tag.Name
	}
	return names
}

func state(conversation restConversation, agents map[string]restAgent) stateOut {
	return stateOut{ID: conversation.ID, Status: conversation.Status, Priority: conversation.Priority,
		Assignee: person(agents, conversation.AssigneeID), Tags: tagNames(conversation.Tags), UpdatedAt: utc(conversation.UpdatedAt)}
}

func conversationRow(conversation restConversation, contact *restContact, agents map[string]restAgent, full bool) conversationOut {
	row := conversationOut{ID: conversation.ID, Status: conversation.Status, Priority: conversation.Priority,
		Assignee: person(agents, conversation.AssigneeID), Tags: tagNames(conversation.Tags), Channel: conversation.Channel, UpdatedAt: utc(conversation.UpdatedAt),
		UntrustedCustomerContent: conversationContent{Subject: conversation.Subject}}
	if contact != nil {
		row.UntrustedCustomerContent.Contact = &contactOut{Name: contact.Name, Email: contact.Email}
		if full {
			row.UntrustedCustomerContent.Contact.Organization = contact.Organization
		}
	}
	if full {
		row.CreatedAt = utc(conversation.CreatedAt)
		row.ContactID = conversation.ContactID
	}
	return row
}

// cut shortens text to at most limit characters and says so inside the text, so the cut survives
// being quoted. It reports whether anything was removed.
func cut(text string, limit int, remedy string) (string, bool) {
	if utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:limit]) + "\n[cut: " + strconv.Itoa(len(runes)-limit) + " more characters. " + remedy + "]", true
}
