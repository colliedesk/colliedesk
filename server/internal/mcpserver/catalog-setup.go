package mcpserver

import (
	"encoding/json"
	"net/http"
)

func setupProgressGet(c *call, _ map[string]any) (any, error) {
	var progress json.RawMessage
	if err := c.get("/api/v1/workspace/setup", nil, &progress); err != nil {
		return nil, err
	}
	return progress, nil
}

func setupGuideSet(c *call, arguments map[string]any) (any, error) {
	var out map[string]any
	if err := c.send(http.MethodPut, "/api/v1/agents/me/setup-guide", map[string]any{"hidden": arguments["hidden"]}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func setupProgressGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_setup_progress_get",
		Title: "Show how far the workspace's setup is",
		Description: `Show the four getting-started steps and where each stands: email (none, waiting, working, needs_attention), import (none, running, done), ` +
			`invite (alone, invited, joined) and first_reply (none, sent; a delivered reply, never a note). The steps come in the order the sign-up goal suggests, ` +
			`and recommended names the one it puts first while it is not done. Computed on every call.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/workspace/setup"},
		Input: noArguments,
		Output: `{"type":"object","required":["steps","complete","recommended"],"properties":{"steps":{"type":"array","items":{"type":"object","required":["key","state","detail"],` +
			`"properties":{"key":{"type":"string"},"state":{"type":"string"},"detail":{"type":["string","null"]}}}},"complete":{"type":"boolean"},"recommended":{"type":["string","null"]}}}`,
		run: setupProgressGet,
	}
}

func setupGuideSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_setup_guide_set",
		Title: "Hide or show the setup guide in the inbox",
		Description: `Hide the setup guide from the inbox of the team member this token acts as, or show it again. Other team members keep their own choice. ` +
			`Safe to repeat. With a read token Colliedesk files it as an agent request.` + "\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"PUT /api/v1/agents/me/setup-guide"},
		Input:  `{"type":"object","additionalProperties":false,"required":["hidden"],"properties":{"hidden":{"type":"boolean"}}}`,
		Output: `{"type":"object","required":["hidden"],"properties":{"hidden":{"type":"boolean"}}}`,
		run:    setupGuideSet,
	}
}
