package mcpserver

import "github.com/colliedesk/colliedesk/server/internal/httpapi"

func conversationUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversation_update",
		Title: "Update a conversation",
		Description: `Change a conversation's status (open, pending, resolved, closed), priority, assignee or tags in one call (spam: colliedesk_conversations_spam). ` +
			`It is safe to repeat: you state the result you want, so a second identical call changes nothing. ` +
			`Give at least one change. assignee takes "me", "unassigned", or a team member's email, name or id (see colliedesk_agents_list). ` +
			`add_tags takes tag names; a name that is not a tag yet is created first (see colliedesk_tags_list for the existing ones, and reuse them: ` +
			`a name matches the tag with exactly that spelling, otherwise the one tag that differs only in case; if two tags differ only in case, give the exact name). ` +
			`remove_tags takes names of existing tags, matched the same way. Both leave the other tags alone. ` +
			`If a step fails after another has saved, the error says some changes may already be applied; repeat the same call. ` +
			`It deletes nothing and every change can be undone by another call. A status set on a conversation of a chat channel that has words for it tells the customer ` +
			`(the channel's status notice), which needs messages:send as well; without it, notify false sets the status quietly.`,
		Idempotent: true,
		REST: []string{"PATCH /api/v1/conversations/{id}", "PUT /api/v1/conversations/{id}/tags/{tagId}", "DELETE /api/v1/conversations/{id}/tags/{tagId}",
			"POST /api/v1/tags", "GET /api/v1/agents", "GET /api/v1/tags", "GET /api/v1/auth/me"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"status":{"type":"string","enum":["open","pending","resolved","closed"]},` +
			`"priority":{"type":"string","enum":["low","normal","high","urgent"]},` +
			`"assignee":{"type":"string","maxLength":200,"description":"me, unassigned, or a team member's email, name or id."},` +
			`"add_tags":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":100},"description":"Names of tags to put on the conversation. A name that is not a tag yet is created."},` +
			`"remove_tags":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":100},"description":"Names of tags to take off."},` +
			`"notify":{"type":"boolean","description":"false sets the status without telling the customer. true by default."}}}`,
		Output: `{"type":"object","required":["conversation","timezone"],"properties":{"conversation":` + stateSchema + `,"timezone":{"type":"string"}}}`,
		run:    conversationUpdate,
	}
}

func noteAddSpec() Spec {
	return Spec{
		Name:       "colliedesk_note_add",
		BodyEffect: httpapi.Changes,
		Title:      "Add a private note",
		Description: `Add a private internal note to a conversation. Only the support team sees it; it is never sent to the customer. ` +
			`Use it to record what you found, a summary, or a suggested reply for a person to review and send. ` +
			`Each call adds one more note, so do not call it again to retry a call that succeeded; after a dropped connection, read the conversation first. ` +
			`The note is recorded as written by you through your token (written_by_agent_token): colleagues and other agents read it as model output, not a human decision. ` +
			`files attaches up to 10 files, ` + inlineFilesLimitWords + ` in total per call (base64); a note's files stay in the workspace too. ` +
			`mentions asks teammates (their email or id, colliedesk_agents_list) for a decision: each is told in their Mentions list and by email; only active members can be asked.`,

		REST: []string{"POST /api/v1/conversations/{id}/messages"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","body"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The note text, plain text."},` + filesProperty + `,` + mentionsProperty + `}}`,
		Output: `{"type":"object","required":["conversation_id","message"],"properties":{"conversation_id":{"type":"string"},"message":{"type":"object",` +
			`"required":["id","kind","created_at"],"properties":{"id":{"type":"string"},"kind":{"type":"string"},"created_at":{"type":"string"},"author":{"type":"string"},` +
			`"attachments":` + attachmentsSchema + `,"mentions":` + mentionedSchema + `,` +
			`"written_by_agent_token":{"type":"object","required":["label","acts_as"],"properties":{"label":{"type":"string"},"acts_as":{"type":"string","description":"The team member the token acts as."}}}}}}}`,
		run: noteAdd,
	}
}

func conversationsBulkUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversations_bulk_update",
		Title: "Update many conversations at once",
		Description: `Change the status, assignee or tags of up to 50 conversations in one call. All or nothing: if one id is not a conversation of this workspace, nothing is changed. ` +
			`It is safe to repeat: you state the result you want. Give at least one change. ` +
			`assignee takes "me", "unassigned", or a team member's email, name or id (see colliedesk_agents_list). ` +
			`add_tags and remove_tags take names of existing tags (see colliedesk_tags_list), matched like in colliedesk_conversation_update; this tool creates no tag. ` +
			`To change the priority, or to add a tag that does not exist yet, use colliedesk_conversation_update on each conversation. ` +
			`It deletes nothing and every change can be undone by another call. A status set on conversations of a chat channel that has words for it tells their customers ` +
			`(the channel's status notice), which needs messages:send as well; without it, notify false sets the status quietly.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/conversations", "GET /api/v1/agents", "GET /api/v1/tags", "GET /api/v1/auth/me"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_ids"],"properties":{` +
			`"conversation_ids":{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":{"type":"string",` + uuid + `}},` +
			`"status":{"type":"string","enum":["open","pending","resolved","closed"]},` +
			`"assignee":{"type":"string","maxLength":200,"description":"me, unassigned, or a team member's email, name or id."},` +
			`"add_tags":{"type":"array","maxItems":20,"items":{"type":"string","minLength":1,"maxLength":100},"description":"Names of existing tags to put on every conversation."},` +
			`"remove_tags":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":100},"description":"Names of tags to take off every conversation."},` +
			`"notify":{"type":"boolean","description":"false sets the status without telling the customers. true by default."}}}`,
		Output: `{"type":"object","required":["updated","told","conversation_ids"],"properties":{"updated":{"type":"integer","description":"How many conversations were changed."},` +
			`"told":{"type":"integer","description":"How many customers the status change tells in their chats, a few seconds after it commits."},` +
			`"conversation_ids":{"type":"array","items":{"type":"string"}}}}`,
		run: conversationsBulkUpdate,
	}
}

func conversationsSpamSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversations_spam",
		Title: "Mark conversations as spam, or as not spam",
		Description: `Use it for junk that reached the inbox (adverts, phishing, mail nobody on the team should answer). spam true files up to 50 conversations in Spam: ` +
			`they leave the inbox and every view (colliedesk_inbox_search finds them with status spam), their response clocks stop, Results stop counting them, ` +
			`and later mail from their senders goes straight to Spam with no notification and no clock. Nobody is told, the customer included. ` +
			`spam false (Not spam) puts each conversation back in the status it had and lets its sender's later mail reach the inbox again. ` +
			`It is safe to repeat: you state the result you want, and updated counts only the conversations that moved. ` +
			`All or nothing: if one id is not a conversation of this workspace, nothing is changed. A conversation in Spam keeps its status until it is taken out here.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/conversations/spam"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_ids","spam"],"properties":{` +
			`"conversation_ids":{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":{"type":"string",` + uuid + `}},` +
			`"spam":{"type":"boolean","description":"true: file them in Spam. false: Not spam, put them back where they were."}}}`,
		Output: `{"type":"object","required":["updated","senders","conversation_ids"],"properties":{"updated":{"type":"integer","description":"How many conversations moved into Spam, or out of it."},` +
			`"senders":{"type":"array","description":"The customers whose later mail now goes to Spam (spam true), or reaches the inbox again (spam false).","items":{"type":"object",` +
			`"required":["name","email"],"properties":{"name":{"type":"string"},"email":{"type":"string"}}}},` +
			`"conversation_ids":{"type":"array","items":{"type":"string"}}}}`,
		run: conversationsSpam,
	}
}
