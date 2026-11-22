package mcpserver

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbListColumnsSchema is a member's columns of the knowledge base article list, as both tools answer it.
var kbListColumnsSchema = `{"type":"object","required":["columns","default"],"properties":{` +
	`"columns":{"type":"array","items":{"type":"string"},"description":"Shown after Article, in this order."},` +
	`"default":{"type":"boolean","description":"True while they have not chosen."}}}`

func myKBListColumnsSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_kb_list_columns_set",
		Title: "Choose your knowledge base list columns",
		Description: `Choose the columns the team member this token acts as sees in the knowledge base article list, in order, after Article; ` +
			`null gives back the default (status, author, updated). It changes only what that person sees, on every device. ` +
			`colliedesk_workspace_settings_get shows the choice as your_kb_list_columns. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/agents/me/kb-list-columns"},
		Input: `{"type":"object","additionalProperties":false,"required":["columns"],"properties":{"columns":{"type":["array","null"],"maxItems":8,"uniqueItems":true,` +
			`"items":{"type":"string","enum":["` + strings.Join(domain.KBListColumnNames, `","`) + `"]}}}}`,
		Output: kbListColumnsSchema,
		run:    myKBListColumnsSet,
	}
}

func myKBListColumnsSet(c *call, arguments map[string]any) (any, error) {
	var out domain.KBListColumns
	if err := c.send(http.MethodPut, "/api/v1/agents/me/kb-list-columns", map[string]any{"columns": arguments["columns"]}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
