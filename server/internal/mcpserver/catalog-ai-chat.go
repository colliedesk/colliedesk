package mcpserver

// The help center's AI chat, as the team reads it: the month's use, the chats and their transcripts.
// Its setup is the help chat channel's (colliedesk_channels_*) and the chat feature's
// (colliedesk_ai_slot_set_request).

const aiChatIDProperty = `"chat_id":{"type":"string",` + uuid + `,"description":"A chat's id, from colliedesk_ai_chat_sessions_list."}`

func aiChatUsageGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_chat_usage_get",
		Title: "Show AI chat usage",
		Description: `Show one UTC month of the help center's AI chat: answers today and this month against the limits, spend (as a share of the plan's limit on the ` +
			`cloud's own provider, in micro-dollars on the workspace's own provider), chats, hand-offs to the team, feedback, and how many published articles it answers from.` +
			"\n\nTier: admin.",
		Tier: "admin", ReadOnly: true,
		REST:   []string{"GET /api/v1/ai/chat/usage"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{"month":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}$","description":"YYYY-MM; the current month when left out."}}}`,
		Output: `{"type":"object","required":["usage"],"properties":{"usage":{"type":"object","required":["month","answers"],"properties":{"month":{"type":"string"},"answers":{"type":"integer"}}}}}`,
		run:    aiChatUsageGet,
	}
}

func aiChatSessionsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_chat_sessions_list",
		Title: "List AI chats",
		Description: `List the end users' chats with the help center's AI, newest activity first, 50 a page: where each started, its state (open, ended, handed_off), ` +
			`how many turns, its first question, whether an answer was rated not helpful, and the conversation a hand-off opened. ` +
			`Filter by channel_id, status or only_not_helpful; pass next_cursor to read on.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/ai/chat/sessions"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"channel_id":{"type":"string",` + uuid + `},` +
			`"status":{"type":"string","enum":["open","ended","handed_off"]},"only_not_helpful":{"type":"boolean"},"cursor":{"type":"string"}}}`,
		Output: `{"type":"object","required":["items","next_cursor"],"properties":{"items":{"type":"array","items":{"type":"object"}},"next_cursor":{"type":["string","null"]}}}`,
		run:    aiChatSessionsList,
	}
}

func aiChatSessionGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_chat_session_get",
		Title: "Read an AI chat",
		Description: `Read one chat with the help center's AI: every question and answer as the end user saw it, the articles each answer cited (number, version, ` +
			`title, as they were), feedback, and the conversation a hand-off opened. Texts are kept 90 days after the last turn.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/ai/chat/sessions/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["chat_id"],"properties":{` + aiChatIDProperty + `}}`,
		Output: `{"type":"object","required":["chat"],"properties":{"chat":{"type":"object","required":["id","status","transcript"]}}}`,
		run:    aiChatSessionGet,
	}
}

func aiChatSessionDeleteRequestSpec() Spec {
	return approvalSpec("colliedesk_ai_chat_session_delete_request", "Ask to delete an AI chat",
		`Ask to delete one chat with the help center's AI and its transcript, for good. A conversation it handed off keeps its own copy.`,
		[]string{"DELETE /api/v1/ai/chat/sessions/{id}"},
		`{"type":"object","additionalProperties":false,"required":["chat_id"],"properties":{`+aiChatIDProperty+`}}`, aiChatSessionDeleteRequest)
}

func aiChatPreviewSpec() Spec {
	return Spec{
		Name:  "colliedesk_ai_chat_preview",
		Title: "Try the AI chat",
		Description: `Ask the help center's AI chat a question as a customer would, with the default help chat channel's instructions (or channel_id's), ` +
			`and get the whole answer as the customer would see it: its text (contact details not in the articles hidden), status (answered, no_source, ` +
			`over_limit) and the articles it cites. Nothing reaches a customer; the call costs like a customer's question and counts toward the chat's limits.` +
			"\n\nTier: draft.",
		Tier:   "draft",
		REST:   []string{"POST /api/v1/ai/chat/preview"},
		Input:  `{"type":"object","additionalProperties":false,"required":["question"],"properties":{"question":{"type":"string","maxLength":1000},"channel_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["answer","status","sources"],"properties":{"answer":{"type":"string"},"status":{"type":"string"},"sources":{"type":"array","items":{"type":"object"}}}}`,
		run:    aiChatPreview,
	}
}
