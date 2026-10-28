package mcpserver

const (
	fixConnectorProperty  = `"connector":{"type":"string","enum":["claude_issue","copilot_issue","cursor_api"],"description":"claude_issue: an issue and an @claude comment for the repository's Claude Code workflow; copilot_issue: an issue assigned to Copilot; cursor_api: a Cursor cloud agent created with the workspace's key (no GitHub confirmation; it leaves within seconds of the approval). Only agents turned on in colliedesk_autofix_settings_get."}`
	fixRowVersionProperty = `"row_version":{"type":"integer","minimum":1,"description":"The row_version you read; a fix that moved since is refused."}`

	fixPreviewSchema = `{"type":"object","required":["untrusted_customer_content","message_ids","notes_allowed","attachments_left_out","removed","destination"],"properties":{` +
		`"untrusted_customer_content":{"type":"object","required":["title","body","notes"],"properties":{"title":{"type":"string"},"body":{"type":"string"},` +
		`"notes":{"type":"array","items":{"type":"object","properties":{"message_id":{"type":"string"},"author_name":{"type":["string","null"]},"created_at":{"type":"string"},"excerpt":{"type":"string"},"included":{"type":"boolean"}}}}}},` +
		`"message_ids":{"type":"array","items":{"type":"string"}},"notes_allowed":{"type":"boolean"},"attachments_left_out":{"type":"integer"},` +
		`"removed":{"type":"object","description":"What masking took out, by kind: emails, phones, cards, secrets, mentions, references."},` +
		`"destination":{"type":"object","properties":{"repo_full_name":{"type":"string"},"connector":{"type":"string"},"sends_as":{"type":"string"},"vendor_retention":{"type":"string"}}}}}`
)

func fixPreviewSpec() Spec {
	return Spec{
		Name:  "colliedesk_fix_preview",
		Title: "Preview what a fix would send",
		Description: `Build what sending a conversation to a coding agent would send, without sending: a title and a body masked on the server (email addresses, phone and card ` +
			`numbers, keys and tokens removed; @mentions and #123 references made inert), each customer message wrapped in <customer-message untrusted="true">, the internal notes ` +
			`the sender may include (note_ids, only when the repository allows notes; never one an agent token wrote) and how many files stay behind. ` +
			`Send it with colliedesk_fix_send_request. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"POST /api/v1/conversations/{id}/fixes/preview"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","repo_id","connector"],"properties":{"conversation_id":{"type":"string",` + uuid + `},` +
			autofixRepoIDProperty + `,` + fixConnectorProperty + `,"note_ids":{"type":"array","maxItems":50,"items":{"type":"string",` + uuid + `}}}}`,
		Output: fixPreviewSchema,
		run:    fixPreview,
	}
}

func fixSendRequestSpec() Spec {
	return approvalSpec("colliedesk_fix_send_request", "Ask a person to send a conversation to a coding agent",
		`Ask a person to send a conversation to a coding agent on a connected repository, with the title and body you reviewed (build them with colliedesk_fix_preview; `+
			`they are masked again on the server). The person who approves confirms on GitHub, and the issue is created with their GitHub account; you never see the address. `+
			`client_key is a new random key per send (the same key sends once). One fix per conversation is in progress at a time; follow it with colliedesk_conversation_fixes_list. `+untrustedNotice,
		[]string{"POST /api/v1/conversations/{id}/fixes"}, fixSendInput(true), fixSendRequest)
}

func fixRetryRequestSpec() Spec {
	return approvalSpec("colliedesk_fix_retry_request", "Ask a person to send a fix again",
		`Ask a person to send a failed or cancelled fix again as a new attempt of its conversation, with the title and body you reviewed (colliedesk_fix_preview, filled in `+
			`from colliedesk_fix_get). The person who approves confirms on GitHub, as for colliedesk_fix_send_request. `+untrustedNotice,
		[]string{"POST /api/v1/autofix/fixes/{id}/retry"}, fixSendInput(false), fixRetryRequest)
}

func fixSendInput(conversation bool) string {
	first := fixIDProperty
	required := `"fix_id"`
	if conversation {
		first, required = `"conversation_id":{"type":"string",`+uuid+`}`, `"conversation_id"`
	}
	return `{"type":"object","additionalProperties":false,"required":[` + required + `,"repo_id","connector","title","body","client_key"],"properties":{` + first + `,` +
		autofixRepoIDProperty + `,` + fixConnectorProperty + `,"title":{"type":"string","minLength":1,"maxLength":256},"body":{"type":"string","minLength":1,"maxLength":65000},` +
		`"message_ids":{"type":"array","maxItems":500,"items":{"type":"string",` + uuid + `}},"note_ids":{"type":"array","maxItems":50,"items":{"type":"string",` + uuid + `}},` +
		`"client_key":{"type":"string","minLength":1,"maxLength":100}}}`
}

func fixCancelSpec() Spec {
	return Spec{
		Name:  "colliedesk_fix_cancel",
		Title: "Cancel an Autofix fix",
		Description: `Cancel a fix in progress (cancelled, cancelled_by_person). Its issue is closed with a comment; an agent already running may still finish on GitHub ` +
			`(stop it there, issue_url), and a pull request already open stays open. Send it again with colliedesk_fix_retry_request. ` + untrustedNotice + "\n\nTier: draft.",
		Tier:   "draft",
		REST:   []string{"POST /api/v1/autofix/fixes/{id}/cancel"},
		Input:  `{"type":"object","additionalProperties":false,"required":["fix_id"],"properties":{` + fixIDProperty + `,` + fixRowVersionProperty + `}}`,
		Output: `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:    fixCancel,
	}
}

func fixDeployedSpec() Spec {
	return Spec{
		Name:        "colliedesk_fix_mark_deployed",
		Title:       "Mark an Autofix fix released",
		Description: `Say a merged fix is released (deployed), so the team can tell the customer. Take it back with colliedesk_fix_unmark_deployed. ` + untrustedNotice + "\n\nTier: draft.",
		Tier:        "draft", Idempotent: true,
		REST:   []string{"PUT /api/v1/autofix/fixes/{id}/deployed"},
		Input:  `{"type":"object","additionalProperties":false,"required":["fix_id"],"properties":{` + fixIDProperty + `,` + fixRowVersionProperty + `}}`,
		Output: `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:    fixMarkDeployed,
	}
}

func fixUndeployedSpec() Spec {
	return Spec{
		Name:        "colliedesk_fix_unmark_deployed",
		Title:       "Take back an Autofix fix's release",
		Description: `Take back a release (merged again): it was rolled back, or marked by mistake. ` + untrustedNotice + "\n\nTier: draft.",
		Tier:        "draft", Idempotent: true,
		REST:   []string{"DELETE /api/v1/autofix/fixes/{id}/deployed"},
		Input:  `{"type":"object","additionalProperties":false,"required":["fix_id"],"properties":{` + fixIDProperty + `}}`,
		Output: `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:    fixUnmarkDeployed,
	}
}

func fixPRLinkSpec() Spec {
	return Spec{
		Name:  "colliedesk_fix_pr_link",
		Title: "Link a pull request to an Autofix fix",
		Description: `Link a pull request Colliedesk did not find by itself (pr_open), from sent, working, or failed with pr_closed. The address must be a pull request of the fix's ` +
			`repository: https://github.com/<owner>/<repo>/pull/<number>. Unlink with colliedesk_fix_pr_unlink. ` + untrustedNotice + "\n\nTier: draft.",
		Tier: "draft",
		REST: []string{"PUT /api/v1/autofix/fixes/{id}/pull-request"},
		Input: `{"type":"object","additionalProperties":false,"required":["fix_id","url"],"properties":{` + fixIDProperty + `,"url":{"type":"string","maxLength":500},` +
			fixRowVersionProperty + `}}`,
		Output: `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:    fixPRLink,
	}
}

func fixPRUnlinkSpec() Spec {
	return Spec{
		Name:        "colliedesk_fix_pr_unlink",
		Title:       "Unlink an Autofix fix's pull request",
		Description: `Unlink a pull request a person linked (back to the stop it was at when the pull request was linked: working or sent); one Colliedesk found by itself stays. ` + untrustedNotice + "\n\nTier: draft.",
		Tier:        "draft",
		REST:        []string{"DELETE /api/v1/autofix/fixes/{id}/pull-request"},
		Input:       `{"type":"object","additionalProperties":false,"required":["fix_id"],"properties":{` + fixIDProperty + `}}`,
		Output:      `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:         fixPRUnlink,
	}
}
