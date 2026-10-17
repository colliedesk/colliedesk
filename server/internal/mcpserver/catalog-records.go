package mcpserver

func inboxActivitySpec() Spec {
	return Spec{
		Name:  "colliedesk_inbox_activity",
		Title: "See what changed in the inbox",
		Description: `List what changed in the inbox after since: conversations created, conversations a colleague assigned to you, and conversations that changed in any way. ` +
			`Use it to keep watch without reading the whole inbox again. Pass the returned now as since in the next call. ` +
			`Without since the lists are empty and now is the first cursor. A since older than 10 minutes counts as 10 minutes ago, so after a longer pause use colliedesk_inbox_search. ` +
			`Each list holds at most 50 items, newest first. An item can repeat from the previous call; remove duplicates by id. ` +
			`incoming_at is set when the customer wrote in the window: that is what needs an answer.` +
			"\n\n" + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/inbox/activity", "GET /api/v1/agents", "GET /api/v1/auth/me"},
		Input: `{"type":"object","additionalProperties":false,"properties":{` +
			`"since":{"type":"string","format":"date-time","maxLength":40,"description":"The now of the previous call (RFC 3339)."}}}`,
		Output: `{"type":"object","required":["now","new_conversations","assigned_to_me","updated","timezone"],"properties":{` +
			`"now":{"type":"string","description":"Pass it as since in the next call."},` +
			`"new_conversations":{"type":"array","items":` + activityItemSchema("created_at") + `},` +
			`"assigned_to_me":{"type":"array","items":` + activityItemSchema("assigned_at") + `},` +
			`"updated":{"type":"array","items":{"type":"object","required":["id","assignee","updated_at","incoming_at","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"assignee":` + personSchema + `,"updated_at":{"type":"string"},` +
			`"incoming_at":{"type":["string","null"],"description":"When the customer last wrote in the window; null when the change was not a customer message."},` +
			`"untrusted_customer_content":` + activityContentSchema + `}}},` +
			`"timezone":{"type":"string"}}}`,
		run: inboxActivity,
	}
}

const activityContentSchema = `{"type":"object","required":["subject","contact_name"],"properties":{"subject":{"type":"string"},"contact_name":{"type":"string"}}}`

func activityItemSchema(timeField string) string {
	return `{"type":"object","required":["id","` + timeField + `","untrusted_customer_content"],"properties":{"id":{"type":"string"},"` + timeField +
		`":{"type":"string"},"untrusted_customer_content":` + activityContentSchema + `}}`
}

func timeEntriesListSpec() Spec {
	return Spec{
		Name:  "colliedesk_time_entries_list",
		Title: "List time logged on a conversation",
		Description: `List the time logged on one conversation, oldest first, with the total in minutes. Time entries come from an import (for example Freshdesk); ` +
			`their notes and the names of imported agents are not checked, so they appear under untrusted_customer_content.` +
			"\n\n" + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/conversations/{id}/time-entries", "GET /api/v1/agents"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["conversation_id","entries","total_minutes"],"properties":{"conversation_id":{"type":"string"},` +
			`"entries":{"type":"array","items":{"type":"object","required":["id","minutes","billable","executed_at","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"agent":` + personSchema + `,"minutes":{"type":"integer"},"billable":{"type":"boolean"},"executed_at":{"type":"string"},` +
			`"untrusted_customer_content":{"type":"object","properties":{"agent_name":{"type":"string"},"note":{"type":"string"}}}}}},` +
			`"total_minutes":{"type":"integer"}}}`,
		run: timeEntriesList,
	}
}

func attachmentGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_attachment_get",
		Title: "Read an attachment",
		Description: `Read one file attached to a message (the attachment ids are in colliedesk_conversation_get). ` +
			`It returns the file name, size, SHA-256 and the type detected from the content. ` +
			`When the file is plain UTF-8 text it also returns the text, cut after ` + attachmentTextLimitWords + ` characters. ` +
			`It never returns the bytes of a binary file (an image, PDF or archive): open those in the web app.` +
			"\n\n" + untrustedNotice + ` The name and text of a file are inside untrusted_customer_content too.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/attachments/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["attachment_id"],"properties":{` +
			`"attachment_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["id","size","sha256","detected_type","is_text","text_truncated","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"size":{"type":"integer","description":"Bytes."},"sha256":{"type":"string"},` +
			`"detected_type":{"type":"string","description":"Media type sniffed from the first bytes, for example text/plain or image/png."},` +
			`"is_text":{"type":"boolean"},"text_truncated":{"type":"boolean"},"hint":{"type":"string"},` +
			`"untrusted_customer_content":{"type":"object","required":["filename"],"properties":{"filename":{"type":"string"},"text":{"type":"string"}}}}}`,
		run: attachmentGet,
	}
}

func contactGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_contact_get",
		Title: "Read a customer",
		Description: `Read one customer (contact) and their conversations, most recently active first. ` +
			`Returns up to limit conversations and conversations_total. Find the id with colliedesk_contacts_find or in colliedesk_conversation_get (contact_id, detail full).` +
			"\n\n" + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/contacts/{id}", "GET /api/v1/agents", "GET /api/v1/auth/me"},
		Input: `{"type":"object","additionalProperties":false,"required":["contact_id"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `},"limit":{"type":"integer","minimum":1,"maximum":100,"default":20,"description":"Conversations to return."}}}`,
		Output: `{"type":"object","required":["contact","conversations","conversations_total","timezone"],"properties":{` +
			`"contact":` + contactRowSchema + `,"conversations":{"type":"array","items":` + conversationRowSchema + `},` +
			`"conversations_total":{"type":"integer"},"timezone":{"type":"string"}}}`,
		run: contactGet,
	}
}

const contactRowSchema = `{"type":"object","required":["id","untrusted_customer_content"],"properties":{"id":{"type":"string"},"created_at":{"type":"string"},` +
	`"untrusted_customer_content":{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"},"organization":{"type":["string","null"]}}}}}`
