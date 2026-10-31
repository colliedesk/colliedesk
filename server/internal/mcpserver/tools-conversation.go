package mcpserver

import (
	"net/url"
	"strings"
)

type getArgs struct {
	ConversationID string `json:"conversation_id"`
	Detail         string `json:"detail"`
}

// untrustedMessage is the part of a message that someone outside the team may have written.
type untrustedMessage struct {
	Author      string   `json:"author,omitempty"`
	From        string   `json:"from,omitempty"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments,omitempty"`
}

// tokenOut names the agent token that wrote a message. The label is what an admin typed when creating it.
type tokenOut struct {
	Label string `json:"label"`
	Scope string `json:"scope"`
}

type messageOut struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
	// Author and WrittenByAgentToken are set for native team messages only. A person's name is shown
	// even when an agent token typed the text, and the token then says so.
	Author              string    `json:"author,omitempty"`
	WrittenByAgentToken *tokenOut `json:"written_by_agent_token,omitempty"`
	Delivery            string    `json:"delivery,omitempty"`
	// NoticeKind names a message the channel sent by itself, not a reply of the team.
	NoticeKind string `json:"notice_kind,omitempty"`
	// Body is text the team wrote in Colliedesk. Any other text is in UntrustedCustomerContent.
	Body                     string            `json:"body,omitempty"`
	Truncated                bool              `json:"truncated"`
	Attachments              []attachmentRef   `json:"attachments,omitempty"`
	UntrustedCustomerContent *untrustedMessage `json:"untrusted_customer_content,omitempty"`
}

// attachmentRef points at a file of a message; its name, which someone outside the team may have
// chosen, stays under untrusted_customer_content or comes from colliedesk_attachment_get.
type attachmentRef struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
}

type conversationGetOut struct {
	Conversation         conversationOut `json:"conversation"`
	Messages             []messageOut    `json:"messages"`
	MessagesTotal        int             `json:"messages_total"`
	MessagesShown        int             `json:"messages_shown"`
	OlderMessagesOmitted int             `json:"older_messages_omitted"`
	// Viewers are the people and other agents who have the conversation open now.
	Viewers  []viewerOut `json:"viewers"`
	Hint     string      `json:"hint,omitempty"`
	Timezone string      `json:"timezone"`
	// LatestReplyNumber is what colliedesk_reply_send takes as seen_reply_number.
	LatestReplyNumber int    `json:"latest_reply_number"`
	Detail            string `json:"detail"`
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
	var activity restActivity
	if err := c.get("/api/v1/inbox/activity", nil, &activity); err != nil {
		return nil, err
	}
	viewers := []viewerOut{}
	for _, item := range activity.Viewing {
		if item.ConversationID == conversation.ID {
			viewers = viewersOut(item.Viewers)
		}
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
		MessagesTotal: len(thread.Items), MessagesShown: len(shown), OlderMessagesOmitted: len(thread.Items) - len(shown), Timezone: me.Workspace.Timezone, Detail: args.Detail, LatestReplyNumber: conversation.LatestReplyNumber}
	anyCut := false
	for _, message := range shown {
		view, wasCut := messageView(message, agents, full)
		anyCut = anyCut || wasCut
		out.Messages = append(out.Messages, view)
	}
	out.Viewers = viewers
	var hints []string
	if anyCut || out.OlderMessagesOmitted > 0 {
		hints = append(hints, `Part of this conversation was left out or cut short. Call colliedesk_conversation_get again with detail=full to read every message in full.`)
	}
	if writing := writingHint(viewers); writing != "" {
		hints = append(hints, writing)
	}
	out.Hint = strings.Join(hints, " ")
	return out, nil
}

func notFoundConversation(id string) string {
	return "No conversation " + id + " in this workspace. Use an id from colliedesk_inbox_search."
}

// nativeTeamMessage reports whether a Colliedesk team member wrote the message in Colliedesk: it has
// an agent as author, and it was not imported or was imported as a reply or private note. Everything
// else is untrusted. A Freshdesk public note or forward can have been written by the customer, and a
// message without an agent author may come from anyone.
func nativeTeamMessage(message restMessage) bool {
	if message.Kind == "incoming" || message.AuthorAgentID == nil {
		return false
	}
	return message.Origin == nil || message.Origin.Kind == "reply" || message.Origin.Kind == "private_note"
}

// authorOf names the team member behind a native team message.
func authorOf(message restMessage, agents map[string]restAgent) string {
	if agent, ok := agents[*message.AuthorAgentID]; ok {
		return agentName(agent)
	}
	return "unknown team member"
}

// tokenOf names the agent token that typed the message, if one did.
func tokenOf(message restMessage) *tokenOut {
	if message.AgentToken == nil {
		return nil
	}
	return &tokenOut{Label: message.AgentToken.Label, Scope: message.AgentToken.Scope}
}

// messageView renders one message; text from outside the team goes under untrusted_customer_content only.
func messageView(message restMessage, agents map[string]restAgent, full bool) (messageOut, bool) {
	body, wasCut := message.Body, false
	if !full {
		body, wasCut = cut(body, conciseBodyRunes, `Ask colliedesk_conversation_get for detail=full to read all of it`)
	}
	view := messageOut{ID: message.ID, Kind: message.Kind, CreatedAt: utc(message.CreatedAt), Truncated: wasCut}
	if message.NoticeKind != nil {
		view.NoticeKind = *message.NoticeKind
	}
	if message.Delivery != nil {
		view.Delivery = message.Delivery.Status
	}
	for _, file := range message.Attachments {
		view.Attachments = append(view.Attachments, attachmentRef{ID: file.ID, Size: file.Size})
	}
	if nativeTeamMessage(message) {
		view.Body = body
		view.Author, view.WrittenByAgentToken = authorOf(message, agents), tokenOf(message)
		return view, wasCut
	}
	files := make([]string, 0, len(message.Attachments))
	for _, file := range message.Attachments {
		files = append(files, file.Filename)
	}
	content := &untrustedMessage{Body: body, Attachments: files}
	if message.Email != nil {
		content.From = message.Email.From
	}
	if message.AuthorName != nil {
		content.Author = strings.TrimSpace(*message.AuthorName)
	}
	view.UntrustedCustomerContent = content
	return view, wasCut
}
