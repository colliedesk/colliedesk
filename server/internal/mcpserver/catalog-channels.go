package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// The tools of every channel (a support address, a web form, later a chat account): the same calls
// for every type, as the web app's Settings > Channels makes them. A type's own actions (verifying an
// email domain) keep their own tools.

const (
	channelIDInput  = `"channel_id":{"type":"string",` + uuid + `,"description":"From colliedesk_channels_list."}`
	channelOutput   = `{"type":"object","required":["channel"],"properties":{"channel":{"type":"object"}}}`
	channelSettings = `"settings":{"type":"object","description":"The channel's rules over its type's defaults: join_policy {kind: always_reopen, within_days with days, never}, ` +
		`default_tags, default_priority, default_assignee_id, rate_limits {window_seconds, per_sender, new_conversations, scope}, commands (name to {action, phrases, tags, priority}), review, scopes, notices {received}. Never a secret."}`
	channelConfig = `"config":{"type":"object","description":"The type's configuration: email {support_address, sender_name}; web_form {allowed_origins}."}`
	heldEventIDs  = `"event_ids":{"type":"array","maxItems":500,"items":{"type":"string",` + uuid + `},"description":"Leave out for every held message."}`
)

func channelsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_channels_list",
		Title: "List the channels",
		Description: `List the workspace's channels (its support email, its web form...) with their state (working, off, needs_attention with a reason code), ` +
			`how many messages each holds because it was off, how many replies were not sent while it was off, and the channel types this server runs with how many of each the workspace may have.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/channels"},
		Input:  noArguments,
		Output: `{"type":"object","required":["channels","types"],"properties":{"channels":{"type":"array","items":{"type":"object"}},"types":{"type":"array","items":{"type":"object"}}}}`,
		run:    channelsList,
	}
}

func channelsGetSpec() Spec {
	return Spec{
		Name: "colliedesk_channels_get", Title: "Show a channel",
		Description: `Show one channel with its configuration as its type shows it (never a secret).` + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/channels/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `}}`,
		Output: channelOutput,
		run:    channelsGet,
	}
}

func channelsEventsSpec() Spec {
	return Spec{
		Name: "colliedesk_channels_events", Title: "Show the history of the channels",
		Description: `Show who added, renamed, turned on or off, changed or removed a channel, changed a secret (only its last four characters), and opened or discarded held messages, newest first. ` +
			`Give channel_id for one channel; give the next_cursor of a page for the page after it.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/channels/events"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"channel_id":{"type":"string",` + uuid + `},` +
			`"cursor":{"type":"string","maxLength":200}}}`,
		Output: `{"type":"object","required":["events","next_cursor"],"properties":{"events":{"type":"array","items":{"type":"object"}},"next_cursor":{"type":["string","null"]}}}`,
		run:    channelsEvents,
	}
}

func channelsCreateSpec() Spec {
	return Spec{
		Name: "colliedesk_channels_create", Title: "Add a channel",
		Description: `Add a channel of a type with its configuration. A type that allows one channel per workspace (email, web_form) refuses a second (channel_type_limit). ` +
			`Adding email connects the support address, like colliedesk_email_channel_update. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin",
		REST: []string{"POST /api/v1/channels"},
		Input: `{"type":"object","additionalProperties":false,"required":["type"],"properties":{"type":{"type":"string","enum":["email","web_form"]},` +
			`"name":{"type":"string","minLength":1,"maxLength":80},"enabled":{"type":"boolean"},` + channelConfig + `,` + channelSettings + `}}`,
		Output: channelOutput,
		run:    channelsCreate,
	}
}

func channelsUpdateSpec() Spec {
	return Spec{
		Name: "colliedesk_channels_update", Title: "Change a channel",
		Description: `Rename a channel, turn it on or off, or change its configuration or its rules (settings). While a channel is off it receives nothing: ` +
			`what arrives is held for a person to open or discard (the web form instead answers as if it did not exist), and replies are saved but not sent until it is on again. ` +
			`It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PATCH /api/v1/channels/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id"],"minProperties":2,"properties":{` + channelIDInput + `,` +
			`"name":{"type":"string","minLength":1,"maxLength":80},"enabled":{"type":"boolean"},` + channelConfig + `,` + channelSettings + `}}`,
		Output: channelOutput,
		run:    channelsUpdate,
	}
}

func channelsCheckSpec() Spec {
	return Spec{
		Name: "colliedesk_channels_check", Title: "Check a channel",
		Description: `Read a channel's state again: working, or what needs attention and why. It changes nothing. ` + belowAdmin + "\n\nTier: admin.",
		Tier:        "admin", Idempotent: true,
		REST:   []string{"POST /api/v1/channels/{id}/check"},
		Input:  `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `}}`,
		Output: channelOutput,
		run:    channelsCheck,
	}
}

func channelsHeldReplaySpec() Spec {
	return Spec{
		Name: "colliedesk_channels_held_replay", Title: "Open the messages a channel held",
		Description: `Open as conversations the messages a channel held while it was off (all, or those of event_ids), as they would have been had it been on; ` +
			`or add them to conversation_id, a conversation of this workspace. Works while the channel is still off. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin",
		REST: []string{"POST /api/v1/channels/{id}/held/replay"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `,` + heldEventIDs + `,` +
			`"conversation_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["results"],"properties":{"results":{"type":"array","items":{"type":"object"}}}}`,
		run:    channelsHeldReplay,
	}
}

func channelsCredentialsSetRequestSpec() Spec {
	return approvalSpec("colliedesk_channels_credentials_set_request", "Ask to set a channel's secret",
		`Ask to store a secret a channel's type needs (an access token of its account). It is sealed and only its last four characters are shown again.`,
		[]string{"PUT /api/v1/channels/{id}/credentials"},
		`{"type":"object","additionalProperties":false,"required":["channel_id","name","value"],"properties":{`+channelIDInput+`,`+
			`"name":{"type":"string","pattern":"^[a-z][a-z0-9_]{0,40}$"},"value":{"type":"string","minLength":1,"maxLength":4096}}}`, channelsCredentialsSetRequest)
}

func channelsDeleteRequestSpec() Spec {
	return approvalSpec("colliedesk_channels_delete_request", "Ask to remove a channel",
		`Ask to remove a channel: its configuration and secrets are deleted, its conversations stay with its name. Removing email disconnects it.`,
		[]string{"DELETE /api/v1/channels/{id}"},
		`{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{`+channelIDInput+`}}`, channelsDeleteRequest)
}

func channelsHeldDiscardRequestSpec() Spec {
	return approvalSpec("colliedesk_channels_held_discard_request", "Ask to discard the messages a channel held",
		`Ask to throw away messages a channel held while it was off (all, or those of event_ids). It cannot be undone; the history keeps how many.`,
		[]string{"POST /api/v1/channels/{id}/held/discard"},
		`{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{`+channelIDInput+`,`+heldEventIDs+`}}`, channelsHeldDiscardRequest)
}

func channelPath(c *call, arguments map[string]any, suffix string) string {
	id, _ := arguments["channel_id"].(string)
	c.notFound = "No channel " + id + " in this workspace. List them with colliedesk_channels_list."
	return "/api/v1/channels/" + url.PathEscape(id) + suffix
}

// channelBody is the call's arguments without the path's id.
func channelBody(arguments map[string]any) map[string]any {
	body := map[string]any{}
	for key, value := range arguments {
		if key != "channel_id" {
			body[key] = value
		}
	}
	return body
}

func channelsList(c *call, _ map[string]any) (any, error) {
	var list struct {
		Items []json.RawMessage `json:"items"`
		Types []json.RawMessage `json:"types"`
	}
	if err := c.get("/api/v1/channels", nil, &list); err != nil {
		return nil, err
	}
	return map[string]any{"channels": list.Items, "types": list.Types}, nil
}

func channelsGet(c *call, arguments map[string]any) (any, error) {
	var channel json.RawMessage
	if err := c.get(channelPath(c, arguments, ""), nil, &channel); err != nil {
		return nil, err
	}
	return map[string]any{"channel": channel}, nil
}

func channelsEvents(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	for _, key := range []string{"channel_id", "cursor"} {
		if value, _ := arguments[key].(string); value != "" {
			query.Set(key, value)
		}
	}
	var page struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor *string           `json:"next_cursor"`
	}
	if err := c.get("/api/v1/channels/events", query, &page); err != nil {
		return nil, err
	}
	return map[string]any{"events": page.Items, "next_cursor": page.NextCursor}, nil
}

func channelsCreate(c *call, arguments map[string]any) (any, error) {
	var channel json.RawMessage
	if err := c.send(http.MethodPost, "/api/v1/channels", arguments, &channel); err != nil {
		return nil, err
	}
	return map[string]any{"channel": channel}, nil
}

func channelsUpdate(c *call, arguments map[string]any) (any, error) {
	var channel json.RawMessage
	if err := c.send(http.MethodPatch, channelPath(c, arguments, ""), channelBody(arguments), &channel); err != nil {
		return nil, err
	}
	return map[string]any{"channel": channel}, nil
}

func channelsCheck(c *call, arguments map[string]any) (any, error) {
	var channel json.RawMessage
	if err := c.send(http.MethodPost, channelPath(c, arguments, "/check"), nil, &channel); err != nil {
		return nil, err
	}
	return map[string]any{"channel": channel}, nil
}

func channelsHeldReplay(c *call, arguments map[string]any) (any, error) {
	var result struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := c.send(http.MethodPost, channelPath(c, arguments, "/held/replay"), channelBody(arguments), &result); err != nil {
		return nil, err
	}
	return map[string]any{"results": result.Results}, nil
}

func channelsCredentialsSetRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPut, channelPath(c, arguments, "/credentials"), channelBody(arguments))
}

func channelsDeleteRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodDelete, channelPath(c, arguments, ""), nil)
}

func channelsHeldDiscardRequest(c *call, arguments map[string]any) (any, error) {
	return ask(c, http.MethodPost, channelPath(c, arguments, "/held/discard"), channelBody(arguments))
}
