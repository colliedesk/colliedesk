package mcpserver

// AI settings (docs/user/ai.md): the server's provider, the providers a workspace adds with its own
// key, and which provider each feature uses. Reading needs a read token and never shows a key. The
// provider tools need an admin token; the two that take a key refuse a lower tier outright. Turning a
// feature on sends ticket data to a provider, so it always waits for a person.

const (
	aiProviderIDProperty = `"provider_id":{"type":"string",` + uuid + `,"description":"From colliedesk_ai_settings_get."}`

	aiProviderSchema = `{"type":"object","required":["id","kind","label","models","check_status","used_by"],"properties":{"id":{"type":"string"},` +
		`"kind":{"type":"string","enum":["anthropic","openai_compatible"]},"label":{"type":"string"},"base_url":{"type":["string","null"]},` +
		`"auth_kind":{"type":"string","enum":["api_key","none"]},"key_hint":{"type":["string","null"],"description":"The key's last four characters; the key is never shown."},` +
		`"models":{"type":"array","items":{"type":"string"},"description":"The first is the one a check uses."},` +
		`"check_status":{"type":"string","enum":["ok","failing"]},"check_error_code":{"type":["string","null"]},"checked_at":{"type":"string"},` +
		`"created_by":{"type":["string","null"]},"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
		`"used_by":{"type":"array","items":{"type":"string","enum":["triage","draft"]}}}}`

	aiSlotSchema = `{"type":"object","required":["slot","source","enabled","active"],"properties":{"slot":{"type":"string","enum":["triage","draft"]},` +
		`"source":{"type":"string","enum":["server","provider","none"]},"provider_id":{"type":["string","null"]},"model":{"type":["string","null"]},` +
		`"effort":{"type":"string","enum":["low","medium","high"]},"enabled":{"type":"boolean"},` +
		`"active":{"type":"boolean","description":"On, and its provider and model are still there: the feature runs."},` +
		`"off_reason":{"type":["string","null"],"description":"Why a feature that is on cannot run: server_provider_missing, model_not_offered."},` +
		`"consented_by":{"type":["string","null"]},"consented_at":{"type":["string","null"]},"updated_by":{"type":["string","null"]},"updated_at":{"type":["string","null"]}}}`

	aiProviderOutput = `{"type":"object","required":["provider"],"properties":{"provider":` + aiProviderSchema + `}}`

	aiKeyNotice = `The key is checked with one small billed request to the first model and saved only when it works; it is sealed on the server and never shown again. ` +
		`It passes through this conversation, so use a key you can revoke at the provider.`
)

func aiSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_settings_get",
		Title: "Show the AI settings",
		Description: `Show the workspace's AI settings: the server's provider (null when the server has none), the providers the workspace added ` +
			`(never a key, only its last four characters), each feature (triage suggestions, reply drafts) with its provider, model and whether it is on ` +
			`and can run, and the newest 50 changes with who made them. Every feature starts off.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/ai"},
		Input: noArguments,
		Output: `{"type":"object","required":["settings"],"properties":{"settings":{"type":"object","required":["server","providers","slots","events"],"properties":{` +
			`"server":{"type":["object","null"]},"providers":{"type":"array","items":` + aiProviderSchema + `},"slots":{"type":"array","items":` + aiSlotSchema + `},` +
			`"events":{"type":"array","items":{"type":"object"}},"provider_limit":{"type":"integer"},"can_store_keys":{"type":"boolean"},"allow_private_base_urls":{"type":"boolean"}}}}}`,
		run: aiSettingsGet,
	}
}

func aiProviderAddSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_provider_add",
		Title: "Add an AI provider",
		Description: `Add a provider with the workspace's own key: anthropic (the Claude API, no base_url) or openai_compatible (OpenAI, OpenRouter, vLLM, Ollama; ` +
			`base_url with its version path, for example https://openrouter.ai/api/v1, public https unless the server operator allows private addresses). ` +
			aiKeyNotice + ` A failed check saves nothing and says why (ai_check_failed with check_error_code). At most 5 providers; adding one turns no feature on.` +
			"\n\nTier: admin (a lower tier is refused, so the key never waits in a request).",
		Tier: "admin",
		REST: []string{"POST /api/v1/ai/providers"},
		Input: `{"type":"object","additionalProperties":false,"required":["kind","label","models"],"properties":{` +
			`"kind":{"type":"string","enum":["anthropic","openai_compatible"]},"label":{"type":"string","minLength":1,"maxLength":80},` +
			`"base_url":{"type":"string","maxLength":500},"api_key":{"type":"string","maxLength":500},` +
			`"models":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","minLength":1,"maxLength":200}}}}`,
		Output: aiProviderOutput,
		run:    aiProviderAdd,
	}
}

func aiProviderUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_provider_update",
		Title: "Change an AI provider",
		Description: `Rename a provider or change its models. A new first model is checked (one billed request) before it is saved. ` +
			`The base URL never changes: a new address is another provider, added with its key, so its features need turning on again. ` +
			`A model a feature uses cannot be removed (ai_model_in_use).` +
			"\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PATCH /api/v1/ai/providers/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["provider_id"],"properties":{` + aiProviderIDProperty + `,` +
			`"label":{"type":"string","minLength":1,"maxLength":80},` +
			`"models":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string","minLength":1,"maxLength":200}}}}`,
		Output: aiProviderOutput,
		run:    aiProviderUpdate,
	}
}

func aiProviderKeySetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_provider_key_set",
		Title: "Replace an AI provider's key",
		Description: `Replace a provider's key. ` + aiKeyNotice + ` When the check fails nothing changes and the old key keeps working.` +
			"\n\nTier: admin (a lower tier is refused, so the key never waits in a request).",
		Tier: "admin",
		REST: []string{"PUT /api/v1/ai/providers/{id}/key"},
		Input: `{"type":"object","additionalProperties":false,"required":["provider_id","api_key"],"properties":{` + aiProviderIDProperty + `,` +
			`"api_key":{"type":"string","minLength":1,"maxLength":500}}}`,
		Output: aiProviderOutput,
		run:    aiProviderKeySet,
	}
}

func aiProviderCheckSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_provider_check",
		Title: "Check an AI provider",
		Description: `Send one small request to a provider's first model and record the result (check_status ok, or failing with check_error_code). ` +
			`The provider bills it; it is counted as a check. At most 10 checks a minute in a workspace.` +
			"\n\nTier: admin.",
		Tier:   "admin",
		REST:   []string{"POST /api/v1/ai/providers/{id}/check"},
		Input:  `{"type":"object","additionalProperties":false,"required":["provider_id"],"properties":{` + aiProviderIDProperty + `}}`,
		Output: aiProviderOutput,
		run:    aiProviderCheck,
	}
}

func aiProviderRemoveSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_provider_remove",
		Title: "Delete an AI provider",
		Description: `Delete a provider and erase its key now. The features that used it turn off and stay off until an admin picks another provider ` +
			`(turned_off lists them). It cannot be undone: there is no trash, so to bring it back add the provider again with its key.` +
			"\n\nTier: admin.",
		Tier: "admin", Destructive: true,
		REST:   []string{"DELETE /api/v1/ai/providers/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["provider_id"],"properties":{` + aiProviderIDProperty + `}}`,
		Output: `{"type":"object","required":["turned_off"],"properties":{"turned_off":{"type":"array","items":{"type":"string","enum":["triage","draft"]}}}}`,
		run:    aiProviderRemove,
	}
}

func aiSlotSetRequestSpec() Spec {
	return approvalSpec("colliedesk_ai_slot_set_request", "Ask to set an AI feature",
		`Ask to set one AI feature (triage suggestions or reply drafts): where it runs (server, provider with provider_id, or none), the model, effort, and on or off. `+
			`Turning a feature on sends that feature's ticket data to its provider, so a person always decides.`,
		[]string{"PUT /api/v1/ai/slots/{slot}"},
		`{"type":"object","additionalProperties":false,"required":["slot","source","enabled"],"properties":{"slot":{"type":"string","enum":["triage","draft"]},`+
			`"source":{"type":"string","enum":["server","provider","none"]},"provider_id":{"type":"string",`+uuid+`},"model":{"type":"string","maxLength":200},`+
			`"effort":{"type":"string","enum":["low","medium","high"],"default":"low"},"enabled":{"type":"boolean"}}}`, aiSlotSetRequest)
}

func aiUsageGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_usage_get",
		Title: "Show AI usage",
		Description: `Show one UTC month of the workspace's AI calls per feature and provider: calls, failed and automatic calls, tokens in and out, ` +
			`and what the server's provider spent in micro-dollars, with this month's allowance when the server sets one. No prompt or answer is ever stored.` +
			"\n\nTier: admin.",
		Tier: "admin", ReadOnly: true,
		REST:  []string{"GET /api/v1/ai/usage"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"month":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}$","description":"YYYY-MM; the current month when left out."}}}`,
		Output: `{"type":"object","required":["usage"],"properties":{"usage":{"type":"object","required":["month","rows"],"properties":{"month":{"type":"string"},` +
			`"allowance":{"type":["object","null"]},"rows":{"type":"array","items":{"type":"object"}}}}}}`,
		run: aiUsageGet,
	}
}

func aiAutomaticLimitsSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_automatic_limits_set",
		Title: "Set the automatic AI limits",
		Description: `Set how many automatic AI tasks (a triage suggestion for each new ticket) the workspace runs a UTC day and a minute; ` +
			`the defaults are 300 and 30. Past the daily limit automatic tasks wait for the next day; what a person asks for is never limited this way.` +
			"\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PUT /api/v1/ai/automatic-limits"},
		Input: `{"type":"object","additionalProperties":false,"required":["per_day","per_minute"],"properties":{` +
			`"per_day":{"type":"integer","minimum":0,"maximum":10000},"per_minute":{"type":"integer","minimum":0,"maximum":1000}}}`,
		Output: `{"type":"object","required":["automatic"],"properties":{"automatic":{"type":"object","required":["per_day","per_minute","used_today"],"properties":{` +
			`"per_day":{"type":"integer"},"per_minute":{"type":"integer"},"used_today":{"type":"integer"},"paused_until":{"type":["string","null"]}}}}}`,
		run: aiAutomaticLimitsSet,
	}
}

const (
	aiDraftConversationProperty = `"conversation_id":{"type":"string",` + uuid + `}`
	aiDraftIDProperty           = `"draft_id":{"type":"string",` + uuid + `,"description":"From colliedesk_ai_draft_create or colliedesk_ai_drafts_list."}`

	aiDraftSchema = `{"type":"object","required":["id","conversation_id","status"],"properties":{"id":{"type":"string"},"conversation_id":{"type":"string"},` +
		`"status":{"type":"string","enum":["queued","running","ready","failed","cancelled"]},"error_code":{"type":["string","null"]},` +
		`"prompt":{"type":"string"},"text":{"type":["string","null"],"description":"The draft, plain text, once ready. Generated text, not something a customer wrote."},` +
		`"missing_info":{"type":["string","null"]},"sources":{"type":"array","items":{"type":"object"}},` +
		`"warnings":{"type":"array","items":{"type":"object"},"description":"An address, a number or another customer's name in the draft that the conversation does not hold."},` +
		`"outcome":{"type":["string","null"]},"similarity":{"type":["number","null"]},"created_at":{"type":"string"},"ready_at":{"type":["string","null"]}}}`

	aiDraftOutput = `{"type":"object","required":["draft"],"properties":{"draft":` + aiDraftSchema + `}}`
)

func aiDraftCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_draft_create",
		Title: "Ask Colliedesk AI for a reply draft",
		Description: `Ask the workspace's AI provider for a draft of a reply to a conversation, with an optional prompt (what the reply should say). ` +
			`It is written in the background from the conversation (never its private notes), matching knowledge base articles and similar solved tickets; ` +
			`poll colliedesk_ai_draft_get with wait. The draft is never sent: a person (or colliedesk_reply_send with ai_draft_id) sends a reply. ` +
			`Fails with ai_feature_off when reply drafts are off, ai_allowance_reached when the included AI is used up.` +
			"\n\nTier: draft.",
		Tier: "draft",
		REST: []string{"POST /api/v1/conversations/{id}/ai-drafts"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + aiDraftConversationProperty + `,` +
			`"prompt":{"type":"string","maxLength":2000}}}`,
		Output: aiDraftOutput,
		run:    aiDraftCreate,
	}
}

func aiDraftGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_draft_get",
		Title: "Show an AI reply draft",
		Description: `Show one AI draft of a conversation: its status (queued, running, ready, failed, cancelled), the text once ready, its sources and warnings. ` +
			`wait (up to 20 seconds) waits while it is being written.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/conversations/{id}/ai-drafts/{draftId}"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","draft_id"],"properties":{` + aiDraftConversationProperty + `,` + aiDraftIDProperty + `,` +
			`"wait":{"type":"integer","minimum":0,"maximum":20,"default":0}}}`,
		Output: aiDraftOutput,
		run:    aiDraftGet,
	}
}

func aiDraftsListSpec() Spec {
	return Spec{
		Name:        "colliedesk_ai_drafts_list",
		Title:       "List a conversation's AI drafts",
		Description: `List a conversation's newest AI drafts (at most 20), newest first, with how each ended.` + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversations/{id}/ai-drafts"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + aiDraftConversationProperty + `}}`,
		Output: `{"type":"object","required":["drafts"],"properties":{"drafts":{"type":"array","items":` + aiDraftSchema + `}}}`,
		run:    aiDraftsList,
	}
}

func aiDraftDiscardSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_draft_discard",
		Title: "Discard an AI reply draft",
		Description: `Stop an AI draft still being written, or discard a ready one. Nothing reaches the customer either way; a new draft can be asked for.` +
			"\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/discard"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id","draft_id"],"properties":{` + aiDraftConversationProperty + `,` + aiDraftIDProperty + `}}`,
		Output: aiDraftOutput,
		run:    aiDraftDiscard,
	}
}

const (
	aiTriageIDProperty = `"suggestion_id":{"type":"string",` + uuid + `,"description":"From colliedesk_ai_triage_get or colliedesk_ai_triage_create."}`

	aiTriageSchema = `{"type":"object","required":["id","conversation_id","status"],"properties":{"id":{"type":"string"},"conversation_id":{"type":"string"},` +
		`"status":{"type":"string","enum":["queued","running","ready","failed","cancelled"]},"error_code":{"type":["string","null"]},"automatic":{"type":"boolean"},` +
		`"tag_ids":{"type":"array","items":{"type":"string"}},"priority":{"type":["string","null"]},"assignee_agent_id":{"type":["string","null"]},` +
		`"reason":{"type":["string","null"],"description":"Generated text, not something a customer wrote."},` +
		`"sources":{"type":"array","description":"The articles and solved conversations the reason rests on.","items":{"type":"object","properties":{"kind":{"type":"string"},"id":{"type":"string"},"title":{"type":"string"}}}},` +
		`"stale":{"type":"object","description":"What a person set before the suggestion came; that field is not suggested."},` +
		`"outcome":{"type":["string","null"],"description":"Null while open, also after a part was applied."},"applied":{"type":"object","description":"What was applied so far."}}}`

	aiTriageOutput = `{"type":"object","required":["suggestion"],"properties":{"suggestion":` + aiTriageSchema + `}}`
)

func aiTriageGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_triage_get",
		Title: "Show a conversation's AI triage suggestion",
		Description: `Show the newest AI triage suggestion of a conversation (tags, priority, assignee, a one-sentence reason), or null. ` +
			`A new ticket gets one by itself when triage is on. Nothing is applied until someone accepts it.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/conversations/{id}/ai-triage"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + aiDraftConversationProperty + `}}`,
		Output: `{"type":"object","required":["suggestion"],"properties":{"suggestion":{"oneOf":[{"type":"null"},` + aiTriageSchema + `]}}}`,
		run:    aiTriageGet,
	}
}

func aiTriageCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_triage_create",
		Title: "Ask Colliedesk AI for a triage suggestion",
		Description: `Ask the workspace's AI provider again for tags, a priority and an assignee for a conversation; it replaces the open suggestion ` +
			`and is written in the background (poll colliedesk_ai_triage_get). Fails with ai_feature_off when triage is off.` +
			"\n\nTier: draft.",
		Tier:   "draft",
		REST:   []string{"POST /api/v1/conversations/{id}/ai-triage"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + aiDraftConversationProperty + `}}`,
		Output: aiTriageOutput,
		run:    aiTriageCreate,
	}
}

func aiTriageAcceptSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_triage_accept",
		Title: "Accept an AI triage suggestion",
		Description: `Apply an open triage suggestion to its conversation: the rest of it, or only the tag_ids, priority or assignee_agent_id you pass (each from the suggestion). ` +
			`A part applied leaves the rest open until it is all applied or dismissed. Tags are added, never removed; undo with colliedesk_conversation_update.` +
			"\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST: []string{"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/accept"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","suggestion_id"],"properties":{` + aiDraftConversationProperty + `,` + aiTriageIDProperty + `,` +
			`"tag_ids":{"type":"array","items":{"type":"string",` + uuid + `}},"priority":{"type":"string","enum":["low","normal","high","urgent"]},"assignee_agent_id":{"type":"string",` + uuid + `}}}`,
		Output: aiTriageOutput,
		run:    aiTriageAccept,
	}
}

func aiTriageDismissSpec() Spec {
	return Spec{
		Name:        "colliedesk_ai_triage_dismiss",
		Title:       "Dismiss an AI triage suggestion",
		Description: `Dismiss the rest of a triage suggestion, or stop one being written. Nothing changes on the conversation.` + "\n\nTier: draft.",
		Tier:        "draft", Idempotent: true,
		REST:   []string{"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/dismiss"},
		Input:  `{"type":"object","additionalProperties":false,"required":["conversation_id","suggestion_id"],"properties":{` + aiDraftConversationProperty + `,` + aiTriageIDProperty + `}}`,
		Output: aiTriageOutput,
		run:    aiTriageDismiss,
	}
}
