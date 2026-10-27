package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

const fixNotFound = "No fix with that id in this workspace. List them with colliedesk_fixes_list."

type fixArguments struct {
	FixID          string `json:"fix_id"`
	ConversationID string `json:"conversation_id"`
	Group          string `json:"group"`
	RepoID         string `json:"repo_id"`
	Connector      string `json:"connector"`
	Limit          int    `json:"limit"`
	Cursor         string `json:"cursor"`
}

// fixForModel moves what was sent (built from the customer's words), the conversation's subject and the
// customer's name under untrusted_customer_content.
func fixForModel(fix map[string]any) map[string]any {
	content := map[string]any{}
	for _, key := range []string{"sent_title", "sent_body", "conversation_subject", "contact_name"} {
		if value, ok := fix[key]; ok {
			content[key] = value
			delete(fix, key)
		}
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
	if args.RepoID != "" {
		query.Set("repo_id", args.RepoID)
	}
	if args.Connector != "" {
		query.Set("connector", args.Connector)
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

func codingAgentPath(arguments map[string]any, rest string) string {
	kind, _ := arguments["kind"].(string)
	return "/api/v1/autofix/connectors/" + url.PathEscape(kind) + rest
}

// codingAgentAnswer runs a change of a coding agent and returns the agent it answers.
func codingAgentAnswer(c *call, method, path string, body any) (any, error) {
	var agent json.RawMessage
	if err := c.send(method, path, body, &agent); err != nil {
		return nil, err
	}
	return map[string]any{"coding_agent": agent}, nil
}

func codingAgentEnableRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPut, codingAgentPath(arguments, ""), map[string]any{"consent_version": arguments["consent_version"]})
}

func codingAgentDisable(c *call, arguments map[string]any) (any, error) {
	return codingAgentAnswer(c, http.MethodDelete, codingAgentPath(arguments, ""), nil)
}

func codingAgentCheck(c *call, arguments map[string]any) (any, error) {
	return codingAgentAnswer(c, http.MethodPost, codingAgentPath(arguments, "/check"), nil)
}

func cursorKeySet(c *call, arguments map[string]any) (any, error) {
	return codingAgentAnswer(c, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": arguments["key"]})
}

func cursorKeyRemove(c *call, _ map[string]any) (any, error) {
	return codingAgentAnswer(c, http.MethodDelete, "/api/v1/autofix/connectors/cursor_api/key", nil)
}
