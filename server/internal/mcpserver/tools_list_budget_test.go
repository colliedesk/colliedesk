package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"
)

// toolsListBudget is the most bytes the tool list may take, as the client receives it: a client that
// loads every tool pays it in context on each session before any work. 574,966 bytes on 2026-11-12
// (246 tools, about half of it output schemas). Never raise it to pass: shorten output schemas and
// descriptions, merge tools, or split the list into toolsets a client chooses
// (docs/internals/performance.md, "MCP tool list budget").
const toolsListBudget = 600_000

func TestToolsListStaysWithinItsBudget(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Budget Desk", "admin@budget.example.com")
	listed, err := e.connect(w.read, "").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > toolsListBudget {
		largest, size := "", 0
		for _, tool := range listed.Tools {
			one, _ := json.Marshal(tool)
			if len(one) > size {
				largest, size = tool.Name, len(one)
			}
		}
		t.Errorf("tools/list is %d bytes for %d tools, over its budget of %d (largest: %s, %d bytes). "+
			"Shorten output schemas and descriptions, merge tools, or split the list into toolsets; never raise the budget",
			len(body), len(listed.Tools), toolsListBudget, largest, size)
	}
}
