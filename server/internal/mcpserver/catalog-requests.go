package mcpserver

// The tools below work with agent requests: calls Colliedesk did not run because they are above the
// token's tier or need a person's approval for every tier. A person approves or rejects them in
// Colliedesk; no tool can, so a model never approves its own request.

import (
	"encoding/json"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

const (
	filedRequestSchema = `{"type":"object","required":["id","kind","status","summary","expires_at"],"properties":{` +
		`"id":{"type":"string"},"kind":{"type":"string","enum":["reply_draft","action"]},"status":{"type":"string"},` +
		`"summary":{"type":"string"},"expires_at":{"type":"string"}}}`

	// filedSchema is what a tool answers when its call was filed as an agent request.
	filedSchema = `{"type":"object","required":["agent_request","next"],"properties":{"agent_request":` + filedRequestSchema + `,"next":{"type":"string"}}}`

	requestSchema = `{"type":"object","required":["id","kind","status","summary","call","requested_by","created_at","expires_at"],"properties":{` +
		`"id":{"type":"string"},"kind":{"type":"string","enum":["reply_draft","action"]},` +
		`"status":{"type":"string","enum":["pending","approved","failed","rejected","cancelled","expired"]},"summary":{"type":"string"},` +
		`"call":{"type":"string","description":"METHOD path that runs when a person approves"},"body":{"description":"The JSON the call carries (a reply draft's text as a person may have edited it)."},` +
		`"conversation_id":{"type":"string"},"requested_by":{"type":"string","description":"Label of the token that asked"},` +
		`"decided_by":{"type":"string"},"decided_at":{"type":"string"},"reason":{"type":"string","description":"Why a person rejected it"},` +
		`"result_status":{"type":"integer","description":"HTTP status of the approved call: 2xx done, anything else failed"},"result_note":{"type":"string"},` +
		`"created_at":{"type":"string"},"expires_at":{"type":"string"}}}`

	requestIDInput = `{"type":"object","additionalProperties":false,"required":["request_id"],"properties":{"request_id":{"type":"string",` + uuid + `}}}`

	// askFirst closes the description of each tool that only files a request.
	askFirst = `It never acts by itself: Colliedesk files an agent request, and the change happens only when a person approves it in Colliedesk (Requests). ` +
		`The result says the request is waiting; follow it with colliedesk_request_get. Each call files one more request, so do not repeat it.`
)

func requestsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_requests_list",
		Title: "List your agent requests",
		Description: `List the agent requests this token filed, pending first, then newest: calls Colliedesk did not run because a person must approve them ` +
			`(above your tier, or approval required for every tier), and what became of them. A request expires after seven days.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/agent-requests"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"status":{"type":"string",` +
			`"enum":["pending","approved","failed","rejected","cancelled","expired"],"description":"Only requests in this state."}}}`,
		Output: `{"type":"object","required":["requests"],"properties":{"requests":{"type":"array","items":` + requestSchema + `}}}`,
		run:    requestsList,
	}
}

func requestGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_request_get",
		Title: "Check an agent request",
		Description: `Read one agent request this token filed: pending, approved (the call ran; result_status says how it went), failed (approved, but the call was refused, ` +
			`for example because the approver may not do it), rejected (with the person's reason), cancelled or expired.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/agent-requests/{id}"},
		Input:  requestIDInput,
		Output: `{"type":"object","required":["request"],"properties":{"request":` + requestSchema + `}}`,
		run:    requestGet,
	}
}

func requestCancelSpec() Spec {
	return Spec{
		Name:  "colliedesk_request_cancel",
		Title: "Withdraw an agent request",
		Description: `Withdraw a pending agent request this token filed, for example one filed by mistake. A cancelled request never runs. ` +
			`A request that is already decided cannot be cancelled.` +
			"\n\nTier: read.",
		Tier:   "read",
		REST:   []string{"POST /api/v1/agent-requests/{id}/cancel"},
		Input:  requestIDInput,
		Output: `{"type":"object","required":["request"],"properties":{"request":` + requestSchema + `}}`,
		run:    requestCancel,
	}
}

func replyDraftSpec() Spec {
	return Spec{
		Name:  "colliedesk_reply_draft",
		Title: "Draft a reply for a person to send",
		Description: `Leave a reply draft on a conversation. Nothing reaches the customer: the draft shows above the reply box, with your token's name, ` +
			`and a person edits it, sends it or discards it. Use it with any tier when a person should check the words before they go out. ` +
			`colliedesk_reply_send with a token below send files the same draft. Each call leaves one more draft. ` +
			`A draft carries no files: a person attaches them when they send it, or a send token attaches them with colliedesk_reply_send.` +
			"\n\nTier: read.",
		Tier: "read",
		REST: []string{"POST /api/v1/conversations/{id}/messages"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","body"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The reply text (Markdown)."},` +
			`"cc":{"type":"array","maxItems":10,"items":{"type":"string","maxLength":254},"description":"Plain email addresses to copy when it is sent."}}}`,
		Output: filedSchema,
		run:    replyDraft,
	}
}

// approvalSpec is a tool for a route that needs a person's approval for every tier.
func approvalSpec(name, title, description string, rest []string, input string, run handler) Spec {
	return Spec{
		Name: name, Title: title, Description: description + " " + askFirst + "\n\nTier: read (a person approves it).",
		Tier: "read", REST: rest, Input: input, Output: filedSchema, run: run,
	}
}

func tokenCreateRequestSpec() Spec {
	return approvalSpec("colliedesk_token_create_request", "Ask for a new agent token",
		`Ask for a new agent token with a label and either a tier (scope) or its own list of permissions (resource:action, for example kb_articles:update). `+
			`The token belongs to the person who approves, who sees it once and passes it on; it is never shown to you.`,
		[]string{"POST /api/v1/auth/tokens"},
		tokenRequestInput(), tokenCreateRequest)
}

func workspaceExportRequestSpec() Spec {
	return approvalSpec("colliedesk_workspace_export_request", "Ask for a workspace export",
		`Ask for an export of all workspace data (a ZIP with every conversation, message, contact and attachment). The person who approves gets the download; it is never sent to you. `+
			`delivery link (the default) asks for a one-time download link, download for the export itself; both give the approver the same file.`,
		[]string{"POST /api/v1/export/link", "GET /api/v1/export"},
		`{"type":"object","additionalProperties":false,"properties":{"delivery":{"type":"string","enum":["link","download"],"default":"link"}}}`, workspaceExportRequest)
}

func billingRequestSpec() Spec {
	return approvalSpec("colliedesk_billing_request", "Ask a person to start a checkout or open billing",
		`Ask for a plan checkout (action checkout, with a plan) or the billing portal (action portal) on the hosted cloud. The person who approves gets the payment page; nothing is paid by you.`,
		[]string{"POST /api/v1/billing/checkout", "POST /api/v1/billing/portal"},
		`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string","enum":["checkout","portal"]},`+
			`"plan":{"type":"string","enum":["solo_monthly","solo_yearly","small_monthly","small_yearly","team_monthly","team_yearly","team25_monthly","team25_yearly","founding"],"description":"For checkout: the plan to buy."}}}`, billingRequest)
}

func emailChannelDisconnectRequestSpec() Spec {
	return approvalSpec("colliedesk_email_channel_disconnect_request", "Ask to disconnect the email channel",
		`Ask to disconnect the workspace's default email channel (is_default): its mail stops coming in and its replies stop going out until an admin connects it again. To remove another channel, use colliedesk_channels_delete_request.`,
		[]string{"DELETE /api/v1/channels/email"}, noArguments, emailChannelDisconnectRequest)
}

func gmailConnectRequestSpec() Spec {
	return approvalSpec("colliedesk_gmail_connect_request", "Ask a person to connect Gmail for sending",
		`Ask an admin to connect a Gmail account to an email channel, so its replies are sent from that address through Gmail and kept in its Sent folder (incoming mail still arrives by forwarding). The person who approves gets Google's consent page and signs in to Google there; you never see it. Connecting again replaces the account. Needs the email channel and the server's Google settings (see colliedesk_email_channel_get, gmail). `+emailChannelChoice,
		[]string{"POST /api/v1/channels/email/gmail", "POST /api/v1/channels/{id}/email/gmail"}, emailChannelInput, gmailConnectRequest)
}

func gmailDisconnectRequestSpec() Spec {
	return approvalSpec("colliedesk_gmail_disconnect_request", "Ask to disconnect Gmail",
		`Ask to disconnect the Gmail account of an email channel: Colliedesk's access is revoked at Google (unless another channel uses the same account) and its replies go out the way they did before Gmail was connected. A reply still waiting to be sent through Gmail fails and can be retried. `+emailChannelChoice,
		[]string{"DELETE /api/v1/channels/email/gmail", "DELETE /api/v1/channels/{id}/email/gmail"}, emailChannelInput, gmailDisconnectRequest)
}

func microsoftConnectRequestSpec() Spec {
	return approvalSpec("colliedesk_microsoft_connect_request", "Ask a person to connect Microsoft 365 for sending",
		`Ask an admin to connect a Microsoft 365 (work or school) or Outlook.com (personal) mailbox to an email channel, so its replies are sent from that address through Microsoft and kept in its Sent Items (incoming mail still arrives by forwarding). The person who approves gets Microsoft's sign-in page and signs in there; you never see it. The mailbox must be the channel's support address; aliases and shared mailboxes are not supported yet. A work account may first need its organization's Microsoft 365 admin to approve Colliedesk (microsoft.admin_consent_url of colliedesk_email_channel_get). Connecting replaces the channel's Gmail or earlier Microsoft 365 connection. Needs the email channel and the server's Microsoft settings (see colliedesk_email_channel_get, microsoft). `+emailChannelChoice,
		[]string{"POST /api/v1/channels/email/microsoft", "POST /api/v1/channels/{id}/email/microsoft"}, emailChannelInput, microsoftConnectRequest)
}

func microsoftDisconnectRequestSpec() Spec {
	return approvalSpec("colliedesk_microsoft_disconnect_request", "Ask to disconnect Microsoft 365",
		`Ask to disconnect the Microsoft 365 mailbox of an email channel: Colliedesk deletes its access, and its replies go out the way they did before Microsoft 365 was connected. Microsoft cannot be asked to revoke it, so Colliedesk stays listed in the account's apps until the person removes it there. A reply still waiting to be sent through Microsoft 365 fails and can be retried. `+emailChannelChoice,
		[]string{"DELETE /api/v1/channels/email/microsoft", "DELETE /api/v1/channels/{id}/email/microsoft"}, emailChannelInput, microsoftDisconnectRequest)
}

func emailSecretRotateRequestSpec() Spec {
	return approvalSpec("colliedesk_email_secret_rotate_request", "Ask to rotate the inbound email secret",
		`Ask for a new inbound secret of an email channel, for example after the old one leaked; mail forwarded with it arrives on that channel. The person who approves sees the new secret once and updates the mail forwarder; it is never shown to you. `+emailChannelChoice,
		[]string{"POST /api/v1/channels/email/secret", "POST /api/v1/channels/{id}/email/secret"}, emailChannelInput, emailSecretRotateRequest)
}

func numberSeriesListSpec() Spec {
	return Spec{
		Name: "colliedesk_number_series_list", Title: "List the conversation number series",
		Description: `List the series the workspace's conversations are numbered in, the default one (#{n}) first: each with its code format, the number the next ` +
			`conversation takes, and where this server took it over (start, start_year). A channel numbers what it opens in the series its settings name (number_series).` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversation-number-series"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","required":["series","format","next","start"],"properties":{"series":{"type":"string"},"format":{"type":"string"},"next":{"type":"integer"},"start":{"type":"integer"},"start_year":{"type":["integer","null"]},"updated_at":{"type":"string"}}}}}}`,
		run:    numberSeriesList,
	}
}

func numberSeriesSetRequestSpec() Spec {
	return approvalSpec("colliedesk_number_series_set_request", "Ask to add or change a conversation number series",
		`Ask to add a number series or change its format (letters, digits, # - _ / . with {n} once, {n:04} to pad, {yyyy} for the year), next number (it only counts on) or start.`,
		[]string{"PUT /api/v1/conversation-number-series/{series}"},
		`{"type":"object","additionalProperties":false,"required":["series"],"minProperties":2,"properties":{"series":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,19}$"},`+
			`"format":{"type":"string","minLength":3,"maxLength":40},"next":{"type":"integer","minimum":1},"start":{"type":"integer","minimum":1},"start_year":{"type":"integer","minimum":1970,"maximum":9999}}}`,
		numberSeriesSetRequest)
}

func responseTargetsGetSpec() Spec {
	return Spec{
		Name: "colliedesk_response_targets_get", Title: "Read the response targets",
		Description: `Read the workspace's response targets: business hours (timezone, weekdays, holidays), first-response and resolution targets by priority in business ` +
			`minutes, how long before a target a conversation is due soon, the channels they apply to and whether reminder emails go out.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/response-targets"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: `{"type":"object","required":["enabled","hours","targets"],"properties":{"enabled":{"type":"boolean"},"timezone":{"type":["string","null"]},"hours":{"type":"array"},"holidays":{"type":"array"},"targets":{"type":"object"},"warn_before_minutes":{"type":"integer"},"channel_ids":{"type":["array","null"]},"alert_email":{"type":"boolean"},"enabled_at":{"type":["string","null"]},"updated_at":{"type":["string","null"]}}}`,
		run:    responseTargetsGet,
	}
}

func responseTargetsSetRequestSpec() Spec {
	return approvalSpec("colliedesk_response_targets_set_request", "Ask to change the response targets",
		`Ask to replace the response targets with a whole document (read it first with colliedesk_response_targets_get and change what you need): enabled, timezone, `+
			`hours (mon to sun, open, from, to), holidays, targets by priority (first_response_minutes, resolution_minutes in business minutes, null for none), `+
			`warn_before_minutes, channel_ids (null for every channel) and alert_email.`,
		[]string{"PUT /api/v1/response-targets"},
		`{"type":"object","additionalProperties":false,"required":["enabled","hours","targets"],"properties":{"enabled":{"type":"boolean"},"timezone":{"type":["string","null"]},`+
			`"hours":{"type":"array","maxItems":7,"items":{"type":"object","required":["day","open"],"properties":{"day":{"type":"string","enum":["mon","tue","wed","thu","fri","sat","sun"]},"open":{"type":"boolean"},"from":{"type":"string"},"to":{"type":"string"}}}},`+
			`"holidays":{"type":"array","maxItems":500,"items":{"type":"object","required":["date","name"],"properties":{"date":{"type":"string"},"name":{"type":"string","maxLength":100}}}},`+
			`"targets":{"type":"object"},"warn_before_minutes":{"type":"integer","minimum":0,"maximum":10080},"channel_ids":{"type":["array","null"],"items":{"type":"string"}},"alert_email":{"type":"boolean"}}}`,
		responseTargetsSetRequest)
}

func workspaceSlugRequestSpec() Spec {
	return approvalSpec("colliedesk_workspace_slug_change_request", "Ask to change the workspace address",
		`Ask to change the workspace's address (slug), which also changes its hosted support email address and web form link.`,
		[]string{"PATCH /api/v1/workspace/slug"},
		`{"type":"object","additionalProperties":false,"required":["slug"],"properties":{"slug":{"type":"string","minLength":1,"maxLength":63}}}`, workspaceSlugRequest)
}

// tokenRequestInput is the input schema of colliedesk_token_create_request, built as JSON so the
// permission list comes from auth.TokenPermissions as data, never pasted into a string.
func tokenRequestInput() string {
	names := []string{}
	for _, item := range auth.TokenPermissions() {
		names = append(names, item.Name)
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"label"},
		"properties": map[string]any{
			"label": map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
			"scope": map[string]any{"type": "string", "enum": auth.ScopeNames,
				"description": "A tier. admin only when the approving person is an admin. Leave out with permissions."},
			"permissions": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "enum": names},
				"description": "A custom token's permissions instead of a tier; an admin-only one needs an admin to approve."},
		},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
