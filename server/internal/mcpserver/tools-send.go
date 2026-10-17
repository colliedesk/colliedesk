package mcpserver

import (
	"net/http"
	"net/url"
	"strings"
)

type replyOut struct {
	ConversationID string `json:"conversation_id"`
	Message        struct {
		ID                  string    `json:"id"`
		Kind                string    `json:"kind"`
		CreatedAt           string    `json:"created_at"`
		Author              string    `json:"author,omitempty"`
		Delivery            string    `json:"delivery,omitempty"`
		CC                  []string  `json:"cc,omitempty"`
		WrittenByAgentToken *tokenOut `json:"written_by_agent_token,omitempty"`
	} `json:"message"`
}

func replySend(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string   `json:"conversation_id"`
		Body           string   `json:"body"`
		CC             []string `json:"cc"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The reply is empty. Write the reply text in body."}
	}
	c.notFound = notFoundConversation(args.ConversationID)
	request := map[string]any{"kind": "outgoing", "body": args.Body}
	if len(args.CC) > 0 {
		request["cc"] = args.CC
	}
	var created restMessage
	if err := c.send(http.MethodPost, "/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/messages", request, &created); err != nil {
		return nil, err
	}
	out := replyOut{ConversationID: args.ConversationID}
	out.Message.ID, out.Message.Kind, out.Message.CreatedAt, out.Message.CC = created.ID, created.Kind, utc(created.CreatedAt), created.CC
	out.Message.WrittenByAgentToken = tokenOf(created)
	if created.Delivery != nil {
		out.Message.Delivery = created.Delivery.Status
	}
	// The reply is saved already, so a failed team lookup only leaves the author's name out.
	if agents, err := c.agentsByID(); err == nil && created.AuthorAgentID != nil {
		out.Message.Author = authorOf(created, agents)
	}
	return out, nil
}

func replyRetry(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ConversationID string `json:"conversation_id"`
		MessageID      string `json:"message_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No reply " + args.MessageID + " on conversation " + args.ConversationID + " in this workspace. Use the ids from colliedesk_conversation_get."
	var retried restMessage
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/messages/" + url.PathEscape(args.MessageID) + "/retry"
	if err := c.send(http.MethodPost, path, nil, &retried); err != nil {
		return nil, err
	}
	delivery := ""
	if retried.Delivery != nil {
		delivery = retried.Delivery.Status
	}
	return map[string]any{"conversation_id": args.ConversationID, "message_id": retried.ID, "delivery": delivery}, nil
}

func conversationCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		ContactID string `json:"contact_id"`
		Subject   string `json:"subject"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Subject) == "" {
		return nil, &toolError{"The subject is empty. Write a subject."}
	}
	c.notFound = "No contact " + args.ContactID + " in this workspace. Use an id from colliedesk_contacts_find."
	var created restConversation
	if err := c.send(http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": args.ContactID, "subject": args.Subject}, &created); err != nil {
		return nil, err
	}
	// The conversation is saved already, so a failed team lookup only leaves the assignee's name out.
	agents, _ := c.agentsByID()
	return map[string]any{"conversation": state(created, agents)}, nil
}

func contactCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	var created restContact
	if err := c.send(http.MethodPost, "/api/v1/contacts", map[string]any{"name": strings.TrimSpace(args.Name), "email": strings.TrimSpace(args.Email)}, &created); err != nil {
		return nil, err
	}
	return map[string]any{"contact": contactRow(created)}, nil
}

func tagRename(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Tag     string `json:"tag"`
		NewName string `json:"new_name"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.NewName) == "" {
		return nil, &toolError{"The new name is empty."}
	}
	tag, err := c.tagByName(args.Tag)
	if err != nil {
		return nil, err
	}
	var renamed restTag
	if err := c.send(http.MethodPatch, "/api/v1/tags/"+url.PathEscape(tag.ID), map[string]any{"name": strings.TrimSpace(args.NewName)}, &renamed); err != nil {
		return nil, err
	}
	return map[string]any{"tag": renamed}, nil
}

func tagDelete(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Tag string `json:"tag"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	tag, err := c.tagByName(args.Tag)
	if err != nil {
		return nil, err
	}
	if err := c.send(http.MethodDelete, "/api/v1/tags/"+url.PathEscape(tag.ID), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": tag}, nil
}

func tokenRevoke(c *call, _ map[string]any) (any, error) {
	if err := c.send(http.MethodPost, "/api/v1/auth/logout", nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"revoked": true, "message": "This token is revoked. Every later call with it fails; a workspace admin can create a new token in Settings."}, nil
}
