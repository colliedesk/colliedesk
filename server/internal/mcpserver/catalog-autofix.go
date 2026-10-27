package mcpserver

const (
	fixIDProperty = `"fix_id":{"type":"string",` + uuid + `,"description":"From colliedesk_fixes_list or colliedesk_conversation_fixes_list."}`

	// fixSchema is one fix as the tools return it: what was sent (built from the customer's words), the
	// conversation's subject and the customer's name sit in untrusted_customer_content.
	fixSchema = `{"type":"object","required":["id","conversation_id","attempt_no","repo_full_name","connector","state","outcome_reason","stale","group","untrusted_customer_content"],"properties":{` +
		`"id":{"type":"string"},"conversation_id":{"type":"string"},"attempt_no":{"type":"integer"},"previous_attempt_id":{"type":["string","null"]},` +
		`"repo_full_name":{"type":"string"},"connector":{"type":"string","enum":["claude_issue","copilot_issue","cursor_api"]},` +
		`"state":{"type":"string","enum":["sending","sent","working","pr_open","merged","deployed","failed","cancelled"]},` +
		`"outcome_reason":{"type":["string","null"],"description":"Why it failed or was cancelled."},` +
		`"stale":{"type":"boolean","description":"Sent 30 minutes ago with no sign of the agent."},"group":{"type":"string","enum":["needs_person","running","done"]},` +
		`"row_version":{"type":"integer"},"issue_url":{"type":["string","null"]},"vendor_url":{"type":["string","null"]},"pr_url":{"type":["string","null"]},` +
		`"merged_at":{"type":["string","null"]},"deployed_at":{"type":["string","null"]},"created_by_name":{"type":"string"},"created_at":{"type":"string"},` +
		`"copilot_assignment":{"type":["string","null"],"enum":["assigned","on_github",null],"description":"Copilot: assigned through the API, or waiting for a person to assign it on GitHub."},` +
		`"untrusted_customer_content":{"type":"object","required":["sent_title","conversation_subject","contact_name"],"properties":{"sent_title":{"type":"string"},"sent_body":{"type":"string"},` +
		`"conversation_subject":{"type":"string"},"contact_name":{"type":["string","null"]}}},` +
		`` +
		`"events":{"type":"array","items":{"type":"object"}}}}`

	fixesDescriptionStates = `States: sending (waiting for the person's GitHub confirmation or the worker), sent (the issue or the Cursor agent exists), ` +
		`working, pr_open, merged, deployed (released, said by a person), failed and cancelled (outcome_reason says why). `
)

func autofixStatusSpec() Spec {
	return Spec{
		Name:  "colliedesk_autofix_status",
		Title: "Show Autofix counts",
		Description: `Show whether Autofix is set up (connected_repos, repositories not paused) and how many fixes wait for a person (failed, merged and not yet released, ` +
			`or sent and silent for 30 minutes) or are running.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/autofix/status"},
		Input:  noArguments,
		Output: `{"type":"object","required":["connected_repos","needs_person","running"],"properties":{"connected_repos":{"type":"integer"},"needs_person":{"type":"integer"},"running":{"type":"integer"}}}`,
		run:    autofixStatus,
	}
}

func fixesListSpec() Spec {
	return Spec{
		Name:  "colliedesk_fixes_list",
		Title: "List Autofix fixes",
		Description: `List the workspace's fixes (conversations sent to a coding agent: Claude or Copilot through a GitHub issue, Cursor through its API), newest first, in pages, ` +
			`optionally one group (needs_person, running or done), one repository (repo_id, from colliedesk_fix_get or the settings) and one agent (connector). ` + fixesDescriptionStates +
			`Read one with colliedesk_fix_get for what was sent and its timeline. ` + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/autofix/fixes"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"group":{"type":"string","enum":["needs_person","running","done"]},` +
			`"repo_id":{"type":"string",` + uuid + `},"connector":{"type":"string","enum":["claude_issue","copilot_issue","cursor_api"]},` +
			`"limit":{"type":"integer","minimum":1,"maximum":100,"default":50},"cursor":{"type":"string","maxLength":200,"description":"next_cursor of the page before."}}}`,
		Output: `{"type":"object","required":["fixes","next_cursor"],"properties":{"fixes":{"type":"array","items":` + fixSchema + `},"next_cursor":{"type":["string","null"]}}}`,
		run:    fixesList,
	}
}

func fixGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_fix_get",
		Title: "Show an Autofix fix",
		Description: `Show one fix: where it is, its issue, Cursor run and pull request links, exactly what was sent (sent_title, sent_body) and its timeline (events). ` +
			fixesDescriptionStates + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/autofix/fixes/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["fix_id"],"properties":{` + fixIDProperty + `}}`,
		Output: `{"type":"object","required":["fix"],"properties":{"fix":` + fixSchema + `}}`,
		run:    fixGet,
	}
}

func conversationFixesListSpec() Spec {
	return Spec{
		Name:        "colliedesk_conversation_fixes_list",
		Title:       "List a conversation's Autofix fixes",
		Description: `List every fix of a conversation, the latest attempt first. ` + fixesDescriptionStates + untrustedNotice + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversations/{id}/fixes"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{"conversation_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["fixes"],"properties":{"fixes":{"type":"array","items":` + fixSchema + `}}}`,
		run:    conversationFixesList,
	}
}

// The coding agents' settings. The names say coding_agent, not fix: they carry no customer text.
const (
	codingAgentKindProperty = `"kind":{"type":"string","enum":["claude_issue","copilot_issue","cursor_api"],` +
		`"description":"claude_issue: Claude through a GitHub issue; copilot_issue: Copilot through a GitHub issue; cursor_api: Cursor through its API with the workspace's key."}`
	codingAgentSchema = `{"type":"object","required":["kind","enabled","mode","consent","credential","last_check"],"properties":{` +
		`"kind":{"type":"string"},"enabled":{"type":"boolean"},"mode":{"type":["string","null"]},` +
		`"consent":{"type":["object","null"],"description":"The data consent on record: version, name, at."},` +
		`"credential":{"type":["object","null"],"description":"Cursor's key: who set it and when, never a character of it."},` +
		`"last_check":{"type":["object","null"],"description":"at, result (ok, key_invalid, no_repo_access, installation_inactive, repo_not_private, provider_unavailable) and message."}}}`
	codingAgentOutput = `{"type":"object","required":["coding_agent"],"properties":{"coding_agent":` + codingAgentSchema + `}}`
	cursorKeyNotice   = `The key is tried with Cursor before it is kept, sealed on the server and never shown again, not one character. ` +
		`It passes through this conversation, so use a key you can revoke at Cursor; a service account key is advised, as a user key acts with all of its owner's rights.`
)

func codingAgentEnableRequestSpec() Spec {
	return approvalSpec("colliedesk_coding_agent_enable_request", "Ask to turn on a coding agent for Autofix",
		`Ask to turn on one coding agent fixes can be sent to (Claude or Copilot through a GitHub issue, Cursor through its API), with the admin's consent to the data it receives. `+
			`consent_version is the version of the consent text (consent_version in the Autofix settings). Turning an agent on lets ticket text leave for that vendor, so a person always decides.`,
		[]string{"PUT /api/v1/autofix/connectors/{kind}"},
		`{"type":"object","additionalProperties":false,"required":["kind","consent_version"],"properties":{`+codingAgentKindProperty+`,`+
			`"consent_version":{"type":"string","minLength":1,"maxLength":40}}}`, codingAgentEnableRequest)
}

func codingAgentDisableSpec() Spec {
	return Spec{
		Name:  "colliedesk_coding_agent_disable",
		Title: "Turn off a coding agent for Autofix",
		Description: `Turn off one coding agent: no new fix goes to it and fixes not sent yet are cancelled (connector_disabled); fixes already sent go on, the consent on record and Cursor's key stay. ` +
			`Turn it on again with colliedesk_coding_agent_enable_request.` + "\n\nTier: admin.",
		Tier:   "admin",
		REST:   []string{"DELETE /api/v1/autofix/connectors/{kind}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["kind"],"properties":{` + codingAgentKindProperty + `}}`,
		Output: codingAgentOutput,
		run:    codingAgentDisable,
	}
}

func codingAgentCheckSpec() Spec {
	return Spec{
		Name:  "colliedesk_coding_agent_check",
		Title: "Check a coding agent for Autofix",
		Description: `Check what Colliedesk can check of one coding agent and keep the result in last_check: for Claude and Copilot, that the GitHub App is installed and the connected repositories are private ` +
			`(the setup on the vendor's side is a checklist a person ticks); for Cursor, that Cursor accepts the key and reaches the connected repositories when it can say.` + "\n\nTier: read.",
		Tier:   "read",
		REST:   []string{"POST /api/v1/autofix/connectors/{kind}/check"},
		Input:  `{"type":"object","additionalProperties":false,"required":["kind"],"properties":{` + codingAgentKindProperty + `}}`,
		Output: codingAgentOutput,
		run:    codingAgentCheck,
	}
}

func cursorKeySetSpec() Spec {
	return Spec{
		Name:  "colliedesk_cursor_key_set",
		Title: "Set the Cursor key for Autofix",
		Description: `Set the workspace's Cursor API key, which fixes sent to Cursor use. ` + cursorKeyNotice +
			"\n\nTier: admin (a lower tier is refused, so the key never waits in a request).",
		Tier:   "admin",
		REST:   []string{"PUT /api/v1/autofix/connectors/cursor_api/key"},
		Input:  `{"type":"object","additionalProperties":false,"required":["key"],"properties":{"key":{"type":"string","minLength":8,"maxLength":500}}}`,
		Output: codingAgentOutput,
		run:    cursorKeySet,
	}
}

func cursorKeyRemoveSpec() Spec {
	return Spec{
		Name:  "colliedesk_cursor_key_remove",
		Title: "Remove the Cursor key of Autofix",
		Description: `Erase the workspace's Cursor key now: Cursor takes no new fix until a key is set again with colliedesk_cursor_key_set; runs already sent go on at Cursor ` +
			`and are followed again once a key is set.` + "\n\nTier: admin.",
		Tier:   "admin",
		REST:   []string{"DELETE /api/v1/autofix/connectors/cursor_api/key"},
		Input:  noArguments,
		Output: codingAgentOutput,
		run:    cursorKeyRemove,
	}
}
