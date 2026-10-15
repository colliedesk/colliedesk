package mcpserver

func conversationUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversation_update",
		Title: "Update a conversation",
		Description: `Change a conversation's status (open, pending, resolved, closed), priority, assignee or tags in one call. ` +
			`It is safe to repeat: you state the result you want, so a second identical call changes nothing. ` +
			`Give at least one change. assignee takes "me", "unassigned", or a team member's email, name or id (see colliedesk_agents_list). ` +
			`add_tags takes tag names; a name that is not a tag yet is created first (see colliedesk_tags_list for the existing ones, and reuse them: ` +
			`names match ignoring case). remove_tags takes names of existing tags. Both leave the other tags alone. ` +
			`It never sends anything to the customer and deletes nothing; every change can be undone by another call.` +
			"\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST: []string{"PATCH /api/v1/conversations/{id}", "PUT /api/v1/conversations/{id}/tags/{tagId}", "DELETE /api/v1/conversations/{id}/tags/{tagId}",
			"POST /api/v1/tags", "GET /api/v1/agents", "GET /api/v1/tags", "GET /api/v1/auth/me"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"status":{"type":"string","enum":["open","pending","resolved","closed"]},` +
			`"priority":{"type":"string","enum":["low","normal","high","urgent"]},` +
			`"assignee":{"type":"string","maxLength":200,"description":"me, unassigned, or a team member's email, name or id."},` +
			`"add_tags":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":100},"description":"Names of tags to put on the conversation. A name that is not a tag yet is created."},` +
			`"remove_tags":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":100},"description":"Names of tags to take off."}}}`,
		Output: `{"type":"object","required":["conversation","timezone"],"properties":{"conversation":` + stateSchema + `,"timezone":{"type":"string"}}}`,
		run:    conversationUpdate,
	}
}

func noteAddSpec() Spec {
	return Spec{
		Name:  "colliedesk_note_add",
		Title: "Add a private note",
		Description: `Add a private internal note to a conversation. Only the support team sees it; it is never sent to the customer. ` +
			`Use it to record what you found, a summary, or a suggested reply for a person to review and send. ` +
			`Each call adds one more note, so do not call it again to retry a call that succeeded.` +
			"\n\nTier: draft.",
		Tier: "draft",
		REST: []string{"POST /api/v1/conversations/{id}/messages"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","body"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The note text, plain text."}}}`,
		Output: `{"type":"object","required":["conversation_id","message"],"properties":{"conversation_id":{"type":"string"},"message":{"type":"object",` +
			`"required":["id","kind","created_at"],"properties":{"id":{"type":"string"},"kind":{"type":"string"},"created_at":{"type":"string"},"author":{"type":"string"}}}}}`,
		run: noteAdd,
	}
}
