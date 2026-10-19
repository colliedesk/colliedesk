package mcpserver

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

type restContactMerge struct {
	ID              string      `json:"id"`
	ContactID       string      `json:"contact_id"`
	MergedContact   restContact `json:"merged_contact"`
	ConversationIDs []string    `json:"conversation_ids"`
	MergedAt        time.Time   `json:"merged_at"`
	UndoneAt        *time.Time  `json:"undone_at"`
}

type contactMergeOut struct {
	MergeID            string        `json:"merge_id"`
	ContactID          string        `json:"contact_id"`
	MergedContact      contactOutRow `json:"merged_contact"`
	ConversationsMoved int           `json:"conversations_moved"`
	MergedAt           string        `json:"merged_at"`
	UndoneAt           *string       `json:"undone_at"`
}

func contactMergeRow(merge restContactMerge) contactMergeOut {
	out := contactMergeOut{MergeID: merge.ID, ContactID: merge.ContactID, MergedContact: contactRow(merge.MergedContact),
		ConversationsMoved: len(merge.ConversationIDs), MergedAt: utc(merge.MergedAt)}
	if merge.UndoneAt != nil {
		undone := utc(*merge.UndoneAt)
		out.UndoneAt = &undone
	}
	return out
}

func notFoundContact(id string) string {
	return "No contact " + id + " in this workspace. Use an id from colliedesk_contacts_find."
}

func contactUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ContactID string `json:"contact_id"`
		Name      string `json:"name"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil, &toolError{"The name is empty. Write the customer's name."}
	}
	c.notFound = notFoundContact(args.ContactID)
	var renamed restContact
	if err := c.send(http.MethodPatch, "/api/v1/contacts/"+url.PathEscape(args.ContactID), map[string]any{"name": strings.TrimSpace(args.Name)}, &renamed); err != nil {
		return nil, err
	}
	return map[string]any{"contact": contactRow(renamed)}, nil
}

func contactMerge(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ContactID          string `json:"contact_id"`
		DuplicateContactID string `json:"duplicate_contact_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No contact " + args.ContactID + " or " + args.DuplicateContactID + " in this workspace. Use ids from colliedesk_contacts_find."
	var merged restContactMerge
	if err := c.send(http.MethodPost, "/api/v1/contacts/"+url.PathEscape(args.ContactID)+"/merges", map[string]any{"contact_id": args.DuplicateContactID}, &merged); err != nil {
		return nil, err
	}
	return map[string]any{"merge": contactMergeRow(merged)}, nil
}

func contactUnmerge(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ContactID string `json:"contact_id"`
		MergeID   string `json:"merge_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No merge " + args.MergeID + " into contact " + args.ContactID + " in this workspace. Use merge_id from colliedesk_contact_get."
	var undone restContactMerge
	if err := c.send(http.MethodDelete, "/api/v1/contacts/"+url.PathEscape(args.ContactID)+"/merges/"+url.PathEscape(args.MergeID), nil, &undone); err != nil {
		return nil, err
	}
	return map[string]any{"merge": contactMergeRow(undone)}, nil
}

func conversationStart(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ContactID  string `json:"contact_id"`
		NewContact *struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"new_contact"`
		Subject string   `json:"subject"`
		Body    string   `json:"body"`
		CC      []string `json:"cc"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Subject) == "" || strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The subject or the message is empty. Write both."}
	}
	if (args.ContactID == "") == (args.NewContact == nil) {
		return nil, &toolError{"Give contact_id or new_contact, not both and not neither."}
	}
	c.notFound = notFoundContact(args.ContactID)
	request := map[string]any{"subject": args.Subject, "body": args.Body}
	if args.NewContact != nil {
		request["contact"] = map[string]any{"name": args.NewContact.Name, "email": args.NewContact.Email}
	} else {
		request["contact_id"] = args.ContactID
	}
	if len(args.CC) > 0 {
		request["cc"] = args.CC
	}
	var created restConversation
	if err := c.send(http.MethodPost, "/api/v1/conversations", request, &created); err != nil {
		return nil, err
	}
	// The conversation and its email are saved already, so a failed lookup only leaves details out.
	agents, _ := c.agentsByID()
	out := map[string]any{"conversation": state(created, agents)}
	var timeline struct {
		Items []restMessage `json:"items"`
	}
	if c.get("/api/v1/conversations/"+url.PathEscape(created.ID)+"/messages", nil, &timeline) == nil && len(timeline.Items) > 0 {
		message := map[string]any{"id": timeline.Items[0].ID}
		if timeline.Items[0].Delivery != nil {
			message["delivery"] = timeline.Items[0].Delivery.Status
		}
		out["message"] = message
	}
	return out, nil
}
