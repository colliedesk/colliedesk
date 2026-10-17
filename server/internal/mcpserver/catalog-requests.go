package mcpserver

// The tools below work with agent requests: calls Colliedesk did not run because they are above the
// token's tier or need a person's approval for every tier. A person approves or rejects them in
// Colliedesk; no tool can, so a model never approves its own request.

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
			`colliedesk_reply_send with a token below send files the same draft. Each call leaves one more draft.` +
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
		`Ask for a new agent token with a label and a tier. The token belongs to the person who approves, who sees it once and passes it on; it is never shown to you.`,
		[]string{"POST /api/v1/auth/tokens"},
		`{"type":"object","additionalProperties":false,"required":["label","scope"],"properties":{"label":{"type":"string","minLength":1,"maxLength":80},`+
			`"scope":{"type":"string","enum":["read","draft","send","admin"],"description":"admin only when the approving person is an admin."}}}`, tokenCreateRequest)
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
			`"plan":{"type":"string","enum":["team_monthly","team_yearly","team25_monthly","team25_yearly","founding"],"description":"For checkout: the plan to buy."}}}`, billingRequest)
}

func emailChannelDisconnectRequestSpec() Spec {
	return approvalSpec("colliedesk_email_channel_disconnect_request", "Ask to disconnect the email channel",
		`Ask to disconnect the workspace's email channel: mail stops coming in and replies stop going out until an admin connects it again.`,
		[]string{"DELETE /api/v1/channels/email"}, noArguments, emailChannelDisconnectRequest)
}

func emailSecretRotateRequestSpec() Spec {
	return approvalSpec("colliedesk_email_secret_rotate_request", "Ask to rotate the inbound email secret",
		`Ask for a new inbound email secret, for example after the old one leaked. The person who approves sees the new secret once and updates the mail forwarder; it is never shown to you.`,
		[]string{"POST /api/v1/channels/email/secret"}, noArguments, emailSecretRotateRequest)
}

func workspaceSlugRequestSpec() Spec {
	return approvalSpec("colliedesk_workspace_slug_change_request", "Ask to change the workspace address",
		`Ask to change the workspace's address (slug), which also changes its hosted support email address and web form link.`,
		[]string{"PATCH /api/v1/workspace/slug"},
		`{"type":"object","additionalProperties":false,"required":["slug"],"properties":{"slug":{"type":"string","minLength":1,"maxLength":63}}}`, workspaceSlugRequest)
}
