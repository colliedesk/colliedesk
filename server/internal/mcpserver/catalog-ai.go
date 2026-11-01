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
		`"used_by":{"type":"array","items":` + aiSlotName + `}}}`

	aiSlotName  = `{"type":"string","enum":["triage","draft","chat","intake"]}`
	aiLevelName = `{"type":"string","enum":["off","suggest","auto"]}`

	aiSlotSchema = `{"type":"object","required":["slot","source","enabled","default_level","active"],"properties":{"slot":` + aiSlotName + `,` +
		`"source":{"type":"string","enum":["server","provider","none"]},"provider_id":{"type":["string","null"]},"model":{"type":["string","null"]},` +
		`"effort":{"type":"string","enum":["low","medium","high"]},"enabled":{"type":"boolean"},` +
		`"default_level":` + aiLevelName + `,"automatic":{"type":"boolean","description":"Deprecated: default_level auto (triage) or suggest (drafts)."},` +
		`"active":{"type":"boolean","description":"On, and its provider and model are still there: the feature runs."},` +
		`"off_reason":{"type":["string","null"],"description":"Why a feature that is on cannot run: server_provider_missing, model_not_offered."},` +
		`"consented_by":{"type":["string","null"]},"consented_at":{"type":["string","null"]},"updated_by":{"type":["string","null"]},"updated_at":{"type":["string","null"]},` +
		`"monthly_cap_micro_usd":{"type":["integer","null"],"description":"AI chat only: its own monthly spending limit in millionths of a US dollar; null is none."}}}`

	aiProviderOutput = `{"type":"object","required":["provider"],"properties":{"provider":` + aiProviderSchema + `}}`

	aiKeyNotice = `The key is checked with one small billed request to the first model and saved only when it works; it is sealed on the server and never shown again. ` +
		`It passes through this conversation, so use a key you can revoke at the provider.`
)

func aiSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_settings_get",
		Title: "Show the AI settings",
		Description: `Show the workspace's AI settings: the server's provider (null when the server has none), the providers the workspace added ` +
			`(never a key, only its last four characters), each feature (triage suggestions, reply drafts, intake of chat messages) with its provider, model, ` +
			`whether it is on and can run, and what it does by itself where no channel says otherwise (default_level), the levels channels set apart from it (channels), ` +
			`the kinds of extra context intake may send (source_consents), and the newest 50 changes with who made them. Every feature starts off.` +
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

func aiModelsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_models_list",
		Title: "List a provider's models",
		Description: `List the models a key may use before adding the provider, to pick the models for colliedesk_ai_provider_add: anthropic (the Claude models, newest first) ` +
			`or openai_compatible (GET {base_url}/models, sorted). Nothing is saved or billed. A provider that cannot list fails with ai_check_failed and check_error_code ` +
			`(model_not_found when the server keeps no list: name the models yourself). The key passes through this conversation, so use a key you can revoke at the provider.` +
			"\n\nTier: admin (a lower tier is refused, so the key never waits in a request).",
		Tier: "admin", ReadOnly: true,
		REST: []string{"POST /api/v1/ai/models"},
		Input: `{"type":"object","additionalProperties":false,"required":["kind"],"properties":{` +
			`"kind":{"type":"string","enum":["anthropic","openai_compatible"]},"base_url":{"type":"string","maxLength":500},"api_key":{"type":"string","maxLength":500}}}`,
		Output: `{"type":"object","required":["models"],"properties":{"models":{"type":"array","items":{"type":"string"}}}}`,
		run:    aiModelsList,
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
		Output: `{"type":"object","required":["turned_off"],"properties":{"turned_off":{"type":"array","items":` + aiSlotName + `}}}`,
		run:    aiProviderRemove,
	}
}

func aiSlotSetRequestSpec() Spec {
	return approvalSpec("colliedesk_ai_slot_set_request", "Ask to set an AI feature",
		`Ask to set one AI feature (triage suggestions, reply drafts, AI chat on the help center, or intake of chat messages): where it runs (server, provider with provider_id, or none), the model, effort, and on or off. `+
			`default_level is what it does by itself where no channel says otherwise: off (only when someone asks), suggest (triage suggests for every new conversation; drafts are written when a customer writes, never sent; intake suggests in Review) `+
			`or auto (triage only: suggestions apply the auto_parts, filling only what nobody set). Left out, it stays as it is; turned on, or moved to another provider, the feature starts at suggest (triage) or off. `+
			`Turning a feature on sends that feature's ticket data to its provider, and automatic apply changes tickets, so a person always decides.`,
		[]string{"PUT /api/v1/ai/slots/{slot}"},
		`{"type":"object","additionalProperties":false,"required":["slot","source","enabled"],"properties":{"slot":`+aiSlotName+`,`+
			`"source":{"type":"string","enum":["server","provider","none"]},"provider_id":{"type":"string",`+uuid+`},"model":{"type":"string","maxLength":200},`+
			`"effort":{"type":"string","enum":["low","medium","high"],"default":"low"},"enabled":{"type":"boolean"},`+
			`"default_level":`+aiLevelName+`,`+
			`"automatic":{"type":"boolean","description":"Deprecated, use default_level: true is auto for triage and suggest for drafts."},`+
			`"auto_parts":{"type":"array","items":{"type":"string","enum":["tags","priority","assignee"]},"description":"Triage only: what applies by itself; tags and priority when left out as automatic turns on."},`+
			`"monthly_cap_micro_usd":{"type":["integer","null"],"minimum":1,"description":"AI chat only: the most it may spend a UTC month in millionths of a US dollar on the workspace's own provider. Left out: kept; null removes it."}}}`, aiSlotSetRequest)
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
			`"per_day":{"type":"integer","minimum":0,"maximum":10000},"per_minute":{"type":"integer","minimum":0,"maximum":1000},` +
			`"draft_per_day":{"type":"integer","minimum":0,"maximum":10000,"description":"Automatic drafts a day, inside per_day; kept when left out."}}}`,
		Output: `{"type":"object","required":["automatic"],"properties":{"automatic":{"type":"object","required":["per_day","per_minute","used_today"],"properties":{` +
			`"per_day":{"type":"integer"},"per_minute":{"type":"integer"},"used_today":{"type":"integer"},"paused_until":{"type":["string","null"]},` +
			`"draft_per_day":{"type":"integer"},"drafts_today":{"type":"integer"},"drafts_paused_until":{"type":["string","null"]},"drafts_reserved_until":{"type":["string","null"]}}}}}`,
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
		`"outcome":{"type":["string","null"],"description":"Null while open, also after a part was applied; auto_applied when AI applied all of it; undone after an undo."},` +
		`"applied":{"type":"object","description":"What was applied so far."},` +
		`"auto_applied_at":{"type":["string","null"]},"auto_applied":{"type":"object","description":"What AI applied by itself; colliedesk_ai_triage_undo takes it back."},` +
		`"skipped":{"type":"object","description":"What AI left because a person had set it, with why."},"kept":{"type":"object","description":"What an undo kept because a person changed it after AI."}}}`

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
		REST:  []string{"GET /api/v1/conversations/{id}/ai-triage"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` + aiDraftConversationProperty + `}}`,
		Output: `{"type":"object","required":["suggestion"],"properties":{"suggestion":{"oneOf":[{"type":"null"},` + aiTriageSchema + `]},` +
			`"auto_applied":{"description":"The newest suggestion AI applied by itself, which a later one asked for by hand does not hide.","oneOf":[{"type":"null"},` + aiTriageSchema + `]}}}`,
		run: aiTriageGet,
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

func aiTriageUndoSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_triage_undo",
		Title: "Undo what AI applied of a triage suggestion",
		Description: `Take back what AI applied of a triage suggestion by itself: the parts named (tags, priority, assignee), or all of it. ` +
			`A part goes back only while it is still what AI set; a part a person changed since is kept, and the suggestion lists it under kept. ` +
			`Undoing again changes nothing; colliedesk_ai_triage_accept applies it again.` +
			"\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST: []string{"POST /api/v1/conversations/{id}/ai-triage/{suggestionId}/undo"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","suggestion_id"],"properties":{` + aiDraftConversationProperty + `,` + aiTriageIDProperty + `,` +
			`"parts":{"type":"array","items":{"type":"string","enum":["tags","priority","assignee"]}}}}`,
		Output: aiTriageOutput,
		run:    aiTriageUndo,
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

const (
	channelAIScopeInput = `"scope":{"type":"string","pattern":"^[a-z_]{0,40}$","description":"'' for the whole channel, or one of its type's scopes (a chat's direct, group): colliedesk_channel_ai_get lists them."}`
	intakeOptionsInput  = `"options":{"type":"object","additionalProperties":false,"description":"Intake only; a field left out keeps its default.","properties":{` +
		`"create_threshold":{"type":"number","minimum":0.5,"maximum":0.99,"description":"Default 0.85."},"attach_threshold":{"type":"number","minimum":0.5,"maximum":0.99,"description":"Default 0.90."},` +
		`"ignore_threshold":{"type":"number","minimum":0.5,"maximum":0.99,"description":"Default 0.95."},` +
		`"attach_requires_single_candidate":{"type":"boolean","description":"Never attach by itself while the customer has two or more open conversations it hesitates between. Default true."},` +
		`"auto_max_age_seconds":{"type":"integer","minimum":60,"maximum":86400,"description":"Older batches are only suggested. Default 7200."},` +
		`"reopen_window_days":{"type":"integer","minimum":0,"maximum":90,"description":"How long a resolved conversation stays a candidate. Default 7."},` +
		`"sources":{"type":"object","additionalProperties":{"type":"boolean"},"description":"Context sources on or off: incoming, thread_window, open_conversations, corrections (off by default), contact, kb, images."},` +
		`"instructions":{"type":"string","maxLength":4000,"description":"The channel's own words added to the prompt."},` +
		`"redact_keywords":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":80},"description":"Words that mark a secret in the channel's language."}}}`
	channelAISchema = `{"type":"object","required":["channel_id","scopes"],"properties":{"channel_id":{"type":"string"},"scopes":{"type":"array","items":{"type":"object","required":["scope","slots"],` +
		`"properties":{"scope":{"type":"string"},"slots":{"type":"array","items":{"type":"object","required":["slot","level","effective_level","source","off_reason"],"properties":{` +
		`"slot":` + aiSlotName + `,"level":{"anyOf":[` + aiLevelName + `,{"type":"null"}],"description":"This scope's own level; null follows the channel, then the workspace."},` +
		`"effective_level":` + aiLevelName + `,"source":{"type":"string","enum":["scope","channel","workspace"]},` +
		`"off_reason":{"type":["string","null"],"description":"workspace_off: the workspace has not turned the feature on, so nothing runs."},"options":{"type":["object","null"]}}}}}}}}}`
)

func channelAIGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_channel_ai_get",
		Title: "Show a channel's AI levels",
		Description: `Show what each AI feature does by itself on a channel and on each scope of its type (a chat's one-to-one messages and groups): ` +
			`its own level (null: it follows the channel, then the workspace's default), the level that runs, and why it is off (workspace_off when the workspace has not turned the feature on). ` +
			`Triage and drafts are on every channel; intake only on a chat channel's scopes, never on email or a web form.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/channels/{id}/ai"},
		Input:  `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{` + channelIDInput + `}}`,
		Output: `{"type":"object","required":["channel_ai"],"properties":{"channel_ai":` + channelAISchema + `}}`,
		run:    channelAIGet,
	}
}

func channelAISetSpec() Spec {
	return Spec{
		Name:  "colliedesk_channel_ai_set",
		Title: "Set a channel's AI level",
		Description: `Set what one AI feature (triage, draft, intake) does by itself on a channel or one of its scopes: off, suggest, auto, or null to follow the channel and the workspace. ` +
			`Drafts go up to suggest (AI never sends); intake exists only on a chat channel's scopes (intake_not_supported otherwise) and takes options. ` +
			`A level only decides the AI's own work; people can still ask by hand while the workspace has the feature on. ` +
			`Lowering a level or tightening intake's options runs at once; a change that lets AI do more by itself (a higher level, a lower threshold, a source turned on, other instructions) ` +
			`is filed as an agent request a person approves, whatever the token holds.` + "\n\nTier: admin (a raise always waits for a person).",
		Tier: "admin", Idempotent: true,
		REST: []string{"PUT /api/v1/channels/{id}/ai/{slot}"},
		Input: `{"type":"object","additionalProperties":false,"required":["channel_id","slot","scope","level"],"properties":{` + channelIDInput + `,"slot":` + aiSlotName + `,` +
			channelAIScopeInput + `,"level":{"anyOf":[` + aiLevelName + `,{"type":"null"}]},` + intakeOptionsInput + `}}`,
		Output: `{"type":"object","properties":{"channel_ai":` + channelAISchema + `,"agent_request":` + filedRequestSchema + `,"next":{"type":"string"}},` +
			`"anyOf":[{"required":["channel_ai"]},{"required":["agent_request","next"]}]}`,
		run: channelAISet,
	}
}

func aiSourceConsentRequestSpec() Spec {
	return approvalSpec("colliedesk_ai_source_consent_request", "Ask to let intake send more data",
		`Ask for an admin's consent that AI intake may send one more kind of context to its provider besides the conversation's text: images (pictures customers sent), `+
			`contact_profile (the customer's name, company and open conversations), or external:<name> (an integration's data). Without it intake leaves that source out.`,
		[]string{"PUT /api/v1/ai/source-consents/{class}"},
		`{"type":"object","additionalProperties":false,"required":["source_class"],"properties":{"source_class":{"type":"string","pattern":"^(images|contact_profile|external:[a-z0-9_]{1,40})$"}}}`, aiSourceConsentRequest)
}

func aiSourceConsentRemoveSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_source_consent_remove",
		Title: "Stop intake sending some data",
		Description: `Take back the consent that AI intake may send a kind of context (images, contact_profile, external:<name>); intake leaves it out from its next decision. ` +
			`removed is false when there was no consent. Giving it again needs colliedesk_ai_source_consent_request.` + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"DELETE /api/v1/ai/source-consents/{class}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["source_class"],"properties":{"source_class":{"type":"string","pattern":"^(images|contact_profile|external:[a-z0-9_]{1,40})$"}}}`,
		Output: `{"type":"object","required":["removed"],"properties":{"removed":{"type":"boolean"}}}`,
		run:    aiSourceConsentRemove,
	}
}
