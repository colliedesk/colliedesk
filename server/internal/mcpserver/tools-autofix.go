package mcpserver

import (
	"encoding/json"
	"net/url"
	"strconv"
)

const fixNotFound = "No fix with that id in this workspace. List them with colliedesk_fixes_list."

type fixArguments struct {
	FixID          string `json:"fix_id"`
	ConversationID string `json:"conversation_id"`
	Group          string `json:"group"`
	Limit          int    `json:"limit"`
	Cursor         string `json:"cursor"`
}

// fixForModel moves what was sent, built from the customer's words, under untrusted_customer_content.
func fixForModel(fix map[string]any) map[string]any {
	content := map[string]any{"sent_title": fix["sent_title"]}
	delete(fix, "sent_title")
	if body, ok := fix["sent_body"]; ok {
		content["sent_body"] = body
		delete(fix, "sent_body")
	}
	fix["untrusted_customer_content"] = content
	return fix
}

func fixesForModel(items []map[string]any) []map[string]any {
	for _, item := range items {
		fixForModel(item)
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items
}

func autofixStatus(c *call, _ map[string]any) (any, error) {
	var status json.RawMessage
	if err := c.get("/api/v1/autofix/status", nil, &status); err != nil {
		return nil, err
	}
	return status, nil
}

func fixesList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixArguments](arguments)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if args.Group != "" {
		query.Set("group", args.Group)
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := c.get("/api/v1/autofix/fixes", query, &page); err != nil {
		return nil, err
	}
	return map[string]any{"fixes": fixesForModel(page.Items), "next_cursor": page.NextCursor}, nil
}

func fixGet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = fixNotFound
	var fix map[string]any
	if err := c.get("/api/v1/autofix/fixes/"+url.PathEscape(args.FixID), nil, &fix); err != nil {
		return nil, err
	}
	return map[string]any{"fix": fixForModel(fix)}, nil
}

func conversationFixesList(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[fixArguments](arguments)
	if err != nil {
		return nil, err
	}
	c.notFound = "No conversation " + args.ConversationID + " in this workspace."
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := c.get("/api/v1/conversations/"+url.PathEscape(args.ConversationID)+"/fixes", nil, &list); err != nil {
		return nil, err
	}
	return map[string]any{"fixes": fixesForModel(list.Items)}, nil
}
