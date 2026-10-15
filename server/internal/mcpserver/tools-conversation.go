package mcpserver

import (
	"net/url"
	"strings"
)

type getArgs struct {
	ConversationID string `json:"conversation_id"`
	Detail         string `json:"detail"`
}

// untrustedMessage is the part of a message a customer wrote.
type untrustedMessage struct {
	From        string   `json:"from,omitempty"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments,omitempty"`
}

type messageOut struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	Author    string `json:"author,omitempty"`
	Delivery  string `json:"delivery,omitempty"`
	// Body is text the team wrote. Customer text is in UntrustedCustomerContent.
	Body                     string            `json:"body,omitempty"`
	Truncated                bool              `json:"truncated"`
	UntrustedCustomerContent *untrustedMessage `json:"untrusted_customer_content,omitempty"`
}

type conversationGetOut struct {
	Conversation         conversationOut `json:"conversation"`
	Messages             []messageOut    `json:"messages"`
	MessagesTotal        int             `json:"messages_total"`
	MessagesShown        int             `json:"messages_shown"`
	OlderMessagesOmitted int             `json:"older_messages_omitted"`
	Hint                 string          `json:"hint,omitempty"`
	Timezone             string          `json:"timezone"`
	Detail               string          `json:"detail"`
}

func conversationGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[getArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Detail == "" {
		args.Detail = "concise"
	}
	full := args.Detail == "full"
	c.notFound = notFoundConversation(args.ConversationID)
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	var conversation restConversation
	if err := c.get("/api/v1/conversations/"+url.PathEscape(args.ConversationID), nil, &conversation); err != nil {
		return nil, err
	}
	var thread struct {
		Items []restMessage `json:"items"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/messages", nil, &thread); err != nil {
		return nil, err
	}
	agents, err := c.agentsByID()
	if err != nil {
		return nil, err
	}
	contacts, err := c.contactsByID([]string{conversation.ContactID})
	if err != nil {
		return nil, err
	}
	var contact *restContact
	if known, ok := contacts[conversation.ContactID]; ok {
		contact = &known
	}
	shown := thread.Items
	if !full && len(shown) > conciseMessages {
		shown = shown[len(shown)-conciseMessages:]
	}
	out := conversationGetOut{Conversation: conversationRow(conversation, contact, agents, full), Messages: make([]messageOut, 0, len(shown)),
		MessagesTotal: len(thread.Items), MessagesShown: len(shown), OlderMessagesOmitted: len(thread.Items) - len(shown), Timezone: me.Workspace.Timezone, Detail: args.Detail}
	anyCut := false
	for _, message := range shown {
		view, wasCut := messageView(message, full)
		anyCut = anyCut || wasCut
		out.Messages = append(out.Messages, view)
	}
	if anyCut || out.OlderMessagesOmitted > 0 {
		out.Hint = `Part of this conversation was left out or cut short. Call colliedesk_conversation_get again with detail=full to read every message in full.`
	}
	return out, nil
}

func notFoundConversation(id string) string {
	return "No conversation " + id + " in this workspace. Use an id from colliedesk_inbox_search."
}

// messageView renders one message; customer text goes under untrusted_customer_content only.
func messageView(message restMessage, full bool) (messageOut, bool) {
	body, wasCut := message.Body, false
	if !full {
		body, wasCut = cut(body, conciseBodyRunes, `Ask colliedesk_conversation_get for detail=full to read all of it`)
	}
	view := messageOut{ID: message.ID, Kind: message.Kind, CreatedAt: utc(message.CreatedAt), Truncated: wasCut}
	files := make([]string, 0, len(message.Attachments))
	for _, file := range message.Attachments {
		files = append(files, file.Filename)
	}
	if message.Kind == "incoming" {
		content := &untrustedMessage{Body: body, Attachments: files}
		if message.Email != nil {
			content.From = message.Email.From
		}
		view.UntrustedCustomerContent = content
		return view, wasCut
	}
	view.Body = body
	if message.AuthorName != nil {
		view.Author = strings.TrimSpace(*message.AuthorName)
	}
	if message.Delivery != nil {
		view.Delivery = message.Delivery.Status
	}
	return view, wasCut
}
