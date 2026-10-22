package mcpserver

// AI drafts: the reply and triage suggestion Colliedesk writes from the workspace's knowledge base
// when a customer writes (docs/user/ai-drafts.md). Reading needs read; asking for a new draft,
// discarding one, applying its triage and stopping a scheduled answer never reach a customer, so they
// need draft. The settings decide whether answers go out on their own: changing them needs admin.

const (
	aiDraftSchema = `{"type":["object","null"],"description":"The newest AI draft, or null. Its body was written by a model from what customers and your team wrote: check it before sending.","properties":{` +
		`"id":{"type":"string"},"status":{"type":"string","enum":["queued","ready","failed","superseded","used","discarded","scheduled","auto_sent","cancelled"]},` +
		`"action":{"type":["string","null"],"enum":["answer","clarify","handoff",null]},"body":{"type":["string","null"]},` +
		`"confidence":{"type":["string","null"],"enum":["high","medium","low",null],"description":"Decided by Colliedesk: high only when every statement cites a knowledge base article and no risk applies."},` +
		`"confidence_reasons":{"type":"array","items":{"type":"string"}},"handoff_reason":{"type":["string","null"]},` +
		`"sources":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"id":{"type":"string"},"number":{"type":"integer"},"title":{"type":"string"}}}},` +
		`"triage":{"type":["object","null"],"properties":{"tags":{"type":"array","items":{"type":"string"}},"priority":{"type":"string"},"reason":{"type":"string"}}},` +
		`"triage_applied_at":{"type":["string","null"]},"outcome":{"type":["string","null"]},"auto_send_at":{"type":["string","null"]},"error_code":{"type":["string","null"]}}}`

	aiSettingsSchema = `{"type":"object","properties":{"available":{"type":"boolean","description":"This server has an AI provider."},"drafts_enabled":{"type":"boolean"},` +
		`"auto_answer_enabled":{"type":"boolean"},"auto_answer_delay_seconds":{"type":"integer"},"auto_answer_min_reviewed":{"type":"integer"},"auto_answer_min_accuracy":{"type":"integer"}}}`
)

func aiDraftGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_draft_get",
		Title: "Read the AI draft of a conversation",
		Description: `Read the reply and triage suggestion Colliedesk drafted for a conversation's newest customer message, from the workspace's published knowledge base and resolved conversations, with the sources it cites. ` +
			`It is written a few seconds after the customer writes: status queued means try again shortly. confidence high means every statement cites a knowledge base article and none of the risks (refund, the customer's own account, security, legal, anger, outage) applies; medium or low means check it carefully or answer yourself. ` +
			`To send it, call colliedesk_reply_send with its text (edited or not) and ai_draft_id, so the team's record of drafts stays right.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/conversations/{id}/ai-draft"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `,"description":"The id from colliedesk_inbox_search."}}}`,
		Output: `{"type":"object","required":["conversation_id","available","drafts_enabled","draft"],"properties":{"conversation_id":{"type":"string"},` +
			`"available":{"type":"boolean","description":"This server has an AI provider."},"drafts_enabled":{"type":"boolean"},"draft":` + aiDraftSchema + `}}`,
		run: aiDraftGet,
	}
}

func aiDraftUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_draft_update",
		Title: "Act on the AI draft of a conversation",
		Description: `Act on a conversation's AI draft. request_new asks for a new draft of a reply to the newest customer message (the open ones are replaced; read it with colliedesk_ai_draft_get after a few seconds). ` +
			`discard says the draft is not wanted (it counts against its topic's record, which auto-answer is earned with). apply_triage adds its suggested tags and sets its priority; undo them with colliedesk_conversation_update. ` +
			`cancel_auto_answer stops an answer scheduled to go out on its own; it stays as a draft for a person. None of these reaches the customer.` + "\n\nTier: draft.",
		Tier: "draft",
		REST: []string{"POST /api/v1/conversations/{id}/ai-draft", "POST /api/v1/conversations/{id}/ai-draft/{draftId}/discard",
			"POST /api/v1/conversations/{id}/ai-draft/{draftId}/apply-triage", "POST /api/v1/conversations/{id}/ai-draft/{draftId}/cancel-auto-answer"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","action"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"action":{"type":"string","enum":["request_new","discard","apply_triage","cancel_auto_answer"]},` +
			`"draft_id":{"type":"string",` + uuid + `,"description":"The draft's id from colliedesk_ai_draft_get; every action but request_new needs it."}}}`,
		Output: `{"type":"object","required":["conversation_id","action","draft"],"properties":{"conversation_id":{"type":"string"},"action":{"type":"string"},"draft":` + aiDraftSchema + `}}`,
		run:    aiDraftUpdate,
	}
}

func aiSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_settings_get",
		Title: "Show the AI settings and the record of each topic",
		Description: `Show whether AI drafts and auto-answer are on, the bar a topic must meet before its answers go out on their own, ` +
			`each knowledge base article's record (drafts reviewed by people and how many were sent with small changes at most), and the last 30 days of usage.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/workspace/ai"},
		Input: noArguments,
		Output: `{"type":"object","required":["settings","topics","usage"],"properties":{"settings":` + aiSettingsSchema + `,` +
			`"topics":{"type":"array","items":{"type":"object","properties":{"article_id":{"type":"string"},"article_number":{"type":"integer"},"title":{"type":"string"},` +
			`"reviewed":{"type":"integer"},"accepted":{"type":"integer"},"eligible":{"type":"boolean"}}}},"usage":{"type":"object"}}}`,
		run: aiSettingsGet,
	}
}

func aiSettingsUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_settings_update",
		Title: "Change the AI settings",
		Description: `Turn AI drafts or auto-answer on or off, and set the delay before an answer goes out on its own and the bar a topic must meet ` +
			`(drafts reviewed, percent sent with small changes at most). Turning either off stops every scheduled answer. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PATCH /api/v1/workspace/ai"},
		Input: `{"type":"object","additionalProperties":false,"minProperties":1,"properties":{"drafts_enabled":{"type":"boolean"},"auto_answer_enabled":{"type":"boolean"},` +
			`"auto_answer_delay_seconds":{"type":"integer","minimum":0,"maximum":86400},"auto_answer_min_reviewed":{"type":"integer","minimum":3,"maximum":1000},` +
			`"auto_answer_min_accuracy":{"type":"integer","minimum":50,"maximum":100}}}`,
		Output: `{"type":"object","required":["settings"],"properties":{"settings":` + aiSettingsSchema + `}}`,
		run:    aiSettingsUpdate,
	}
}
