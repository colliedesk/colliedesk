package mcpserver

import (
	"encoding/json"
	"net/url"
	"strconv"
)

// Chat threads: what was said in a place of a chat channel (a one-to-one chat, a group), the customers'
// messages, the team's and the channel's own, whether or not they became a conversation's. Everything a
// sender wrote and their name come back under untrusted_customer_content.

const (
	chatItemSchema = `{"type":"object","required":["id","thread_id","direction","role","disposition","occurred_at","untrusted_customer_content"],"properties":{` +
		`"id":{"type":"string"},"thread_id":{"type":"string"},"direction":{"type":"string","enum":["in","out"]},"role":{"type":"string","enum":["customer","staff","bot"]},` +
		`"contact_id":{"type":["string","null"]},"agent_id":{"type":["string","null"]},"is_other_customer":{"type":"boolean","description":"A customer who is not the conversation's (a group)."},` +
		`"message_id":{"type":["string","null"],"description":"The conversation message it became, read with colliedesk_conversation_get."},` +
		`"disposition":{"type":"string","enum":["unresolved","gathering","conversation","pending_review","not_request","staff","bot","archived"]},` +
		`"disposition_reason":{"type":["string","null"]},"occurred_at":{"type":"string"},"linked":{"type":"boolean"},"link_reason":{"type":["string","null"]},` +
		`"body":{"type":"string","description":"What the team or the channel said; a customer's words are under untrusted_customer_content."},` +
		`"untrusted_customer_content":{"type":"object","properties":{"sender_name":{"type":"string"},"sender":{"type":"string"},"body":{"type":"string"},` +
		`"files":{"type":"array","items":{"type":"string"}},"quote_external_id":{"type":"string"}}}}}`
	chatPageOutput = `{"type":"object","required":["items","next_cursor"],"properties":{"items":{"type":"array","items":` + chatItemSchema + `},"next_cursor":{"type":["string","null"]}}}`
	chatPaging     = `"cursor":{"type":"string","maxLength":200,"description":"next_cursor of the page before: older items."},"limit":{"type":"integer","minimum":1,"maximum":100,"default":100}`
)

func conversationChatGetSpec() Spec {
	return Spec{
		Name: "colliedesk_conversation_chat_get", Title: "Read a conversation's chat",
		Description: `Read what was said in the chat places (a one-to-one chat, a group) a conversation's messages came from, newest first: the customers' messages, ` +
			`the team's replies and the channel's notices, with which items are part of this conversation (linked) and how they came to it. ` +
			`linked only keeps the conversation's own. Another customer of a group is marked is_other_customer. Empty for a conversation that came from no chat. ` +
			untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/conversations/{id}/chat"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{"conversation_id":{"type":"string",` + uuid + `},` +
			`"linked":{"type":"string","enum":["only"]},` + chatPaging + `}}`,
		Output: chatPageOutput,
		run:    conversationChatGet,
	}
}

func channelThreadsListSpec() Spec {
	return Spec{
		Name: "colliedesk_channel_threads_list", Title: "List a chat channel's places",
		Description: `List the places of a chat channel (one-to-one chats and groups), most recently active first, each with its newest item. ` +
			`kind keeps direct or group ones. Read a place with colliedesk_channel_thread_items. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/channels/{id}/threads"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `,` +
			`"kind":{"type":"string","enum":["direct","group"]},` + chatPaging + `}}`,
		Output: `{"type":"object","required":["items","next_cursor"],"properties":{"items":{"type":"array","items":{"type":"object","required":["id","kind","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"kind":{"type":"string","enum":["direct","group"]},"contact_id":{"type":["string","null"]},"opens_conversations":{"type":"boolean"},` +
			`"last_item_at":{"type":["string","null"]},"last_item":{"anyOf":[` + chatItemSchema + `,{"type":"null"}]},` +
			`"untrusted_customer_content":{"type":"object","properties":{"name":{"type":"string"},"external_id":{"type":"string"}}}}}},"next_cursor":{"type":["string","null"]}}}`,
		run: channelThreadsList,
	}
}

func channelSendersSpec() Spec {
	return Spec{
		Name: "colliedesk_channel_senders", Title: "List who wrote in a chat channel",
		Description: `List the people who wrote in a chat channel's places (customers and the team, never the channel itself), each once, most recent first, at most 200: ` +
			`kind and value name them as the channel does (a Zalo user id), with the place and time they last wrote. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/channels/{id}/senders"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `}}`,
		Output: `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","required":["kind","value","role","last_at","thread_id","untrusted_customer_content"],"properties":{` +
			`"kind":{"type":"string"},"value":{"type":"string"},"role":{"type":"string","enum":["customer","staff"]},"last_at":{"type":"string"},"thread_id":{"type":"string"},` +
			`"untrusted_customer_content":{"type":"object","properties":{"name":{"type":"string"},"thread_name":{"type":"string"}}}}}}}}`,
		run: channelSenders,
	}
}

func channelThreadItemsSpec() Spec {
	return Spec{
		Name: "colliedesk_channel_thread_items", Title: "Read a chat place",
		Description: `Read everything said in one place of a chat channel, newest first: what became a conversation's, what is waiting for review, and what was set aside ` +
			`as not a request. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/channels/{id}/threads/{threadId}/items"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id","thread_id"],"properties":{` + channelIDInput + `,` +
			`"thread_id":{"type":"string",` + uuid + `,"description":"From colliedesk_channel_threads_list."},` + chatPaging + `}}`,
		Output: chatPageOutput,
		run:    channelThreadItems,
	}
}

func channelUnresolvedItemsSpec() Spec {
	return Spec{
		Name: "colliedesk_channel_unresolved_items", Title: "List chat messages nobody concluded on",
		Description: `List what customers said on chat channels, in places that open conversations, that nobody concluded on yet (it arrived while the channel was off), ` +
			`oldest first; older_than keeps those older than so many seconds. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/channel-thread-items"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"older_than":{"type":"integer","minimum":0,"maximum":2592000},` +
			`"cursor":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":100,"default":100}}}`,
		Output: chatPageOutput,
		run:    channelUnresolvedItems,
	}
}

type chatItemJSON struct {
	ID                string  `json:"id"`
	ThreadID          string  `json:"thread_id"`
	Direction         string  `json:"direction"`
	Role              string  `json:"role"`
	ContactID         *string `json:"contact_id"`
	AgentID           *string `json:"agent_id"`
	IsOtherCustomer   bool    `json:"is_other_customer"`
	Body              string  `json:"body"`
	QuoteExternalID   *string `json:"quote_external_id"`
	MessageID         *string `json:"message_id"`
	Disposition       string  `json:"disposition"`
	DispositionReason *string `json:"disposition_reason"`
	OccurredAt        string  `json:"occurred_at"`
	Linked            bool    `json:"linked"`
	LinkReason        *string `json:"link_reason"`
	Sender            struct {
		Kind  *string `json:"kind"`
		Value *string `json:"value"`
		Name  *string `json:"name"`
	} `json:"sender"`
	Files []struct {
		Name string `json:"name"`
	} `json:"files"`
}

// chatItemOut puts what a sender wrote under untrusted_customer_content; only the team's and the
// channel's own words stay outside it.
func chatItemOut(item chatItemJSON) map[string]any {
	untrusted := map[string]any{}
	if item.Sender.Name != nil {
		untrusted["sender_name"] = *item.Sender.Name
	}
	if item.Sender.Value != nil {
		untrusted["sender"] = *item.Sender.Value
	}
	if item.QuoteExternalID != nil {
		untrusted["quote_external_id"] = *item.QuoteExternalID
	}
	files := make([]string, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(files, file.Name)
	}
	untrusted["files"] = files
	out := map[string]any{"id": item.ID, "thread_id": item.ThreadID, "direction": item.Direction, "role": item.Role, "contact_id": item.ContactID,
		"agent_id": item.AgentID, "is_other_customer": item.IsOtherCustomer, "message_id": item.MessageID, "disposition": item.Disposition,
		"disposition_reason": item.DispositionReason, "occurred_at": item.OccurredAt, "linked": item.Linked, "link_reason": item.LinkReason,
		"untrusted_customer_content": untrusted}
	// A team member typing on the channel's own app, or the channel, is still text from outside Colliedesk
	// unless Colliedesk sent it.
	if item.Direction == "out" {
		out["body"] = item.Body
	} else {
		untrusted["body"] = item.Body
	}
	return out
}

func chatPageOut(raw json.RawMessage) (any, error) {
	var page struct {
		Items []chatItemJSON `json:"items"`
		Next  *string        `json:"next_cursor"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, chatItemOut(item))
	}
	return map[string]any{"items": items, "next_cursor": page.Next}, nil
}

func pagingQuery(arguments map[string]any, keys ...string) url.Values {
	query := url.Values{}
	for _, key := range append(keys, "cursor") {
		if value, ok := arguments[key].(string); ok && value != "" {
			query.Set(key, value)
		}
	}
	for _, key := range []string{"limit", "older_than"} {
		if value, ok := arguments[key].(float64); ok {
			query.Set(key, strconv.Itoa(int(value)))
		}
	}
	return query
}

func conversationChatGet(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["conversation_id"].(string)
	var raw json.RawMessage
	if err := c.get("/api/v1/conversations/"+url.PathEscape(id)+"/chat", pagingQuery(arguments, "linked"), &raw); err != nil {
		return nil, err
	}
	return chatPageOut(raw)
}

func channelThreadsList(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	c.notFound = channelNotFound
	var page struct {
		Items []struct {
			ID                 string        `json:"id"`
			Kind               string        `json:"kind"`
			ExternalID         string        `json:"external_id"`
			Name               *string       `json:"name"`
			ContactID          *string       `json:"contact_id"`
			OpensConversations bool          `json:"opens_conversations"`
			LastItemAt         *string       `json:"last_item_at"`
			LastItem           *chatItemJSON `json:"last_item"`
		} `json:"items"`
		Next *string `json:"next_cursor"`
	}
	if err := c.get("/api/v1/channels/"+url.PathEscape(id)+"/threads", pagingQuery(arguments, "kind"), &page); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, thread := range page.Items {
		untrusted := map[string]any{"external_id": thread.ExternalID}
		if thread.Name != nil {
			untrusted["name"] = *thread.Name
		}
		var last any
		if thread.LastItem != nil {
			last = chatItemOut(*thread.LastItem)
		}
		items = append(items, map[string]any{"id": thread.ID, "kind": thread.Kind, "contact_id": thread.ContactID, "opens_conversations": thread.OpensConversations,
			"last_item_at": thread.LastItemAt, "last_item": last, "untrusted_customer_content": untrusted})
	}
	return map[string]any{"items": items, "next_cursor": page.Next}, nil
}

func channelSenders(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	c.notFound = channelNotFound
	var page struct {
		Items []struct {
			Kind       string  `json:"kind"`
			Value      string  `json:"value"`
			Name       *string `json:"name"`
			Role       string  `json:"role"`
			LastAt     string  `json:"last_at"`
			ThreadID   string  `json:"thread_id"`
			ThreadName *string `json:"thread_name"`
		} `json:"items"`
	}
	if err := c.get("/api/v1/channels/"+url.PathEscape(id)+"/senders", nil, &page); err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, sender := range page.Items {
		untrusted := map[string]any{}
		if sender.Name != nil {
			untrusted["name"] = *sender.Name
		}
		if sender.ThreadName != nil {
			untrusted["thread_name"] = *sender.ThreadName
		}
		items = append(items, map[string]any{"kind": sender.Kind, "value": sender.Value, "role": sender.Role, "last_at": sender.LastAt,
			"thread_id": sender.ThreadID, "untrusted_customer_content": untrusted})
	}
	return map[string]any{"items": items}, nil
}

func channelThreadItems(c *call, arguments map[string]any) (any, error) {
	id, _ := arguments["channel_id"].(string)
	thread, _ := arguments["thread_id"].(string)
	var raw json.RawMessage
	if err := c.get("/api/v1/channels/"+url.PathEscape(id)+"/threads/"+url.PathEscape(thread)+"/items", pagingQuery(arguments), &raw); err != nil {
		return nil, err
	}
	return chatPageOut(raw)
}

func channelUnresolvedItems(c *call, arguments map[string]any) (any, error) {
	var raw json.RawMessage
	if err := c.get("/api/v1/channel-thread-items", pagingQuery(arguments), &raw); err != nil {
		return nil, err
	}
	return chatPageOut(raw)
}
