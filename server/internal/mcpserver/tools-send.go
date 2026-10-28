package mcpserver

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type replyOut struct {
	ConversationID string `json:"conversation_id"`
	// Warning says a person had a reply to this customer open and not sent yet.
	Warning string `json:"warning,omitempty"`
	Message struct {
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
		ConversationID  string   `json:"conversation_id"`
		Body            string   `json:"body"`
		CC              []string `json:"cc"`
		SeenReplyNumber *int     `json:"seen_reply_number"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Body) == "" {
		return nil, &toolError{"The reply is empty. Write the reply text in body."}
	}
	if args.SeenReplyNumber == nil {
		return nil, &toolError{"Pass seen_reply_number: the latest_reply_number of colliedesk_conversation_get for this conversation. Nothing was sent."}
	}
	c.notFound = notFoundConversation(args.ConversationID)
	request := map[string]any{"kind": "outgoing", "body": args.Body, "seen_reply_number": *args.SeenReplyNumber}
	if len(args.CC) > 0 {
		request["cc"] = args.CC
	}
	path := "/api/v1/conversations/" + url.PathEscape(args.ConversationID) + "/messages"
	var created restMessage
	err = c.rest.do(c.ctx, c.token, http.MethodPost, path, nil, request, &created)
	if failure, ok := err.(*restError); ok && failure.Status == http.StatusConflict && failure.Code == "reply_conflict" {
		return nil, c.replyConflict(args.ConversationID, *args.SeenReplyNumber)
	}
	if err == nil {
		c.wrote = true
	}
	if err := c.refuse(err, c.notFound); err != nil {
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
	// Nor does a failed look at who else has the conversation open.
	var activity restActivity
	if err := c.get("/api/v1/inbox/activity", nil, &activity); err == nil {
		for _, item := range activity.Viewing {
			if item.ConversationID == args.ConversationID {
				out.Warning = writingWarning(viewersOut(item.Viewers))
			}
		}
	}
	return out, nil
}

// writingWarning names the people who had a reply to this customer open and not sent when the agent's went.
func writingWarning(viewers []viewerOut) string {
	var people []string
	for _, viewer := range viewers {
		if viewer.Writing && viewer.AgentToken == nil {
			people = append(people, viewer.Name)
		}
	}
	if len(people) == 0 {
		return ""
	}
	return strings.Join(people, ", ") + ` had a reply to this customer open and not sent yet when yours went. Tell the user, so the customer does not get two answers.`
}

// replyConflict words a reply refused because someone replied after seen: what they wrote (team text;
// an imported or customer-written message is only pointed at), and what to do.
func (c *call) replyConflict(conversationID string, seen int) error {
	text := "Someone replied to this conversation after the version you read (seen_reply_number " + strconv.Itoa(seen) + "). Nothing was sent."
	var thread struct {
		Items []restMessage `json:"items"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(conversationID)+"/messages", nil, &thread); err == nil {
		agents, _ := c.agentsByID()
		for _, message := range thread.Items {
			if message.Kind != "outgoing" || message.ReplyNumber == nil || *message.ReplyNumber <= seen {
				continue
			}
			text += "\n\nReply " + strconv.Itoa(*message.ReplyNumber) + " at " + utc(message.CreatedAt)
			if nativeTeamMessage(message) {
				text += " by " + authorOf(message, agents) + ":\n" + message.Body
			} else {
				text += ", imported from another helpdesk: read it with colliedesk_conversation_get."
			}
		}
	}
	return &toolError{text + "\n\nRead the conversation again with colliedesk_conversation_get, and send only if your reply still fits, with its new latest_reply_number."}
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
		ChannelID string `json:"channel_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Subject) == "" {
		return nil, &toolError{"The subject is empty. Write a subject."}
	}
	c.notFound = "No contact " + args.ContactID + " in this workspace. Use an id from colliedesk_contacts_find."
	var created restConversation
	request := map[string]any{"contact_id": args.ContactID, "subject": args.Subject}
	if args.ChannelID != "" {
		request["channel_id"] = args.ChannelID
	}
	if err := c.send(http.MethodPost, "/api/v1/conversations", request, &created); err != nil {
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
