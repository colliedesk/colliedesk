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
