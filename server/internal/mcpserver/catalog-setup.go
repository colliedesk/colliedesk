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
		Description: `Show the six getting-started steps, each with its name and where it stands: email (none, waiting, working, needs_attention), ` +
			`import (none, running, done), ai_drafts (none, on, nothing_to_read: off with no import and no published article to draft from, ` +
			`unavailable: off with no built-in AI on this server and no provider of the workspace's own), reply_targets (none, on), ` +
			`invite (alone, invited, joined) and first_reply (none, sent; a delivered reply, never a note). The steps come in the order the sign-up goal suggests, ` +
			`and recommended names the one it puts first while it is not done. complete counts email, import, invite and first_reply only: ` +
			`ai_drafts and reply_targets are offers that never hold the setup back. ` +
			`reply_target_presets lists the reply targets presets in business minutes, a day being reply_target_hours.day_minutes; ` +
			`reply_target_suggestion names the one that fits the team (from the median first reply of the imported history), or is null when ` +
			`reply targets are on or this token may not change them. ` +
			`To finish ai_drafts call colliedesk_ai_slot_set (slot draft, enabled true, default_level suggest) with source server when ` +
			`colliedesk_ai_settings_get shows the server's built-in AI offers drafts (included in the plan), else source provider with provider_id ` +
			`(one of the workspace's own providers, which bills the workspace); a slot that already has a source keeps it. To finish reply_targets ` +
			`call colliedesk_response_targets_set with a preset's targets, enabled true, warn_before_minutes 30 and the hours in reply_target_hours: ` +
			`when kept is true they are the hours already set in Settings, which a preset never replaces; otherwise Monday to Friday, 09:00 to 18:00. ` +
			`Computed on every call.`,
		REST:  []string{"GET /api/v1/workspace/setup"},
		Input: noArguments,
		Output: `{"type":"object","required":["steps","complete","recommended","reply_target_presets","reply_target_hours","reply_target_suggestion"],"properties":{` +
			`"steps":{"type":"array","items":{"type":"object","required":["key","name","state","detail"],` +
			`"properties":{"key":{"type":"string"},"name":{"type":"string"},"state":{"type":"string"},"detail":{"type":["string","null"]}}}},` +
			`"complete":{"type":"boolean"},"recommended":{"type":["string","null"]},` +
			`"reply_target_presets":{"type":"array","items":{"type":"object","required":["key","name","targets"],"properties":{"key":{"type":"string"},"name":{"type":"string"},"targets":{"type":"object"}}}},` +
			`"reply_target_hours":{"type":"object","required":["kept","day_minutes","hours"],"properties":{"kept":{"type":"boolean"},"day_minutes":{"type":"integer"},` +
			`"hours":{"type":"array","items":{"type":"object","required":["day","open"],"properties":{"day":{"type":"string"},"open":{"type":"boolean"},"from":{"type":"string"},"to":{"type":"string"}}}}}},` +
			`"reply_target_suggestion":{"type":["object","null"],"required":["preset","basis","median_first_reply_minutes"],"properties":{"preset":{"type":"string"},"basis":{"type":"string"},"median_first_reply_minutes":{"type":["integer","null"]}}}}}`,
		run: setupProgressGet,
	}
}

func setupGuideSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_setup_guide_set",
		Title: "Hide or show the setup guide in the inbox",
		Description: `Hide the setup guide from the inbox of the team member this token acts as, or show it again. Other team members keep their own choice. ` +
			`Safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/agents/me/setup-guide"},
		Input:      `{"type":"object","additionalProperties":false,"required":["hidden"],"properties":{"hidden":{"type":"boolean"}}}`,
		Output:     `{"type":"object","required":["hidden"],"properties":{"hidden":{"type":"boolean"}}}`,
		run:        setupGuideSet,
	}
}
