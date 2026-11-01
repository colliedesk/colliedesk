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
		Description: `Show what to do next in setup in one call. For leave_helpdesk, returns six ordered move steps: import, preview, custom_domain (TXT proof), ` +
			`plan, cname (a successful address check) and search_console (verification and sitemap confirmed; detail verification_present when a Google tag is already stored). Each includes state, detail and url; next_url links to ` +
			`the first unfinished step, including a missing prerequisite. Done milestones stay done, skipped domain steps can be restored. Other goals keep the four ` +
			`email/import/invite/first_reply steps. No external calls. Use colliedesk_setup_move_step_set for confirmations or skipping.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/workspace/setup"},
		Input:  noArguments,
		Output: setupProgressOutput,
		run:    setupProgressGet,
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

const setupProgressOutput = `{"type":"object","required":["steps","complete","recommended","next_url"],"properties":{"steps":{"type":"array","items":{"type":"object","required":["key","state","detail","url"],"properties":{"key":{"type":"string"},"state":{"type":"string"},"detail":{"type":["string","null"]},"url":{"type":"string"}}}},"complete":{"type":"boolean"},"recommended":{"type":["string","null"]},"next_url":{"type":["string","null"]}}}`

func setupMoveStepSetSpec() Spec {
	return Spec{
		Name: "colliedesk_setup_move_step_set", Title: "Confirm or skip a move step",
		Description: `After checking the public preview, confirm preview done. After keeping Search Console verification and submitting the custom domain sitemap, confirm search_console done. ` +
			`The server checks prerequisites. Set custom_domain skipped when no custom domain is wanted, or none to restore skipped steps. Done stays done. ` +
			`Safe to repeat. The answer includes all steps and the next URL. A lower-tier token files a request for an admin.` + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"PUT /api/v1/workspace/setup/move"},
		Input:  `{"type":"object","additionalProperties":false,"required":["step","state"],"properties":{"step":{"type":"string","enum":["preview","custom_domain","search_console"]},"state":{"type":"string","enum":["done","skipped","none"]}}}`,
		Output: setupProgressOutput,
		run: func(c *call, args map[string]any) (any, error) {
			var out json.RawMessage
			if err := c.send(http.MethodPut, "/api/v1/workspace/setup/move", args, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}
