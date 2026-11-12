package mcpserver

const (
	mentionsProperty = `"mentions":{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":320},` +
		`"description":"Teammates to ask, each their email or id (colliedesk_agents_list). Only active members of the workspace; you are never asked by your own note."}`

	mentionedSchema = `{"type":"array","items":{"type":"object","required":["name","by_email"],"properties":{"agent_id":{"type":"string"},"name":{"type":"string"},` +
		`"by_email":{"type":"boolean","description":"false: they turned mention emails off; it is in their Mentions list only."},"read_at":{"type":["string","null"]}}}}`
)

func noteMentionSpec() Spec {
	return Spec{
		Name:  "colliedesk_note_mention",
		Title: "Ask more teammates on a note",
		Description: `Ask more teammates on a private note you wrote (the person your token acts as wrote it), for example when the first person you asked is away. ` +
			`Only the people the note did not ask yet are told, once each, in their Mentions list and by email; a repeat tells nobody again. ` +
			`A mention cannot be taken back. To ask on a new matter, write a new note with colliedesk_note_add and its mentions.`,
		Idempotent: true,
		REST:       []string{"POST /api/v1/conversations/{id}/messages/{messageId}/mentions"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","note_id","mentions"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},"note_id":{"type":"string",` + uuid + `,"description":"The note's message id."},` + mentionsProperty + `}}`,
		Output: `{"type":"object","required":["conversation_id","note_id","mentions"],"properties":{"conversation_id":{"type":"string"},"note_id":{"type":"string"},"mentions":` + mentionedSchema + `}}`,
		run:    noteMention,
	}
}

func mentionsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_mentions_list",
		Title: "List the notes that ask you",
		Description: `List the private notes in which a teammate asked the person your token acts as (@mention), newest first: who asked, the conversation, the start of the note. ` +
			`Use it to find what the team is waiting on you for; open a conversation with colliedesk_conversation_get, answer with colliedesk_note_add, ` +
			`then clear the mention with colliedesk_mentions_mark_read. Unread only by default; status all lists every one. ` + untrustedNotice,
		REST: []string{"GET /api/v1/mentions"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"status":{"type":"string","enum":["unread","all"],"default":"unread"},` +
			`"limit":{"type":"integer","minimum":1,"maximum":100,"default":50},"before":{"type":"string",` + uuid + `,"description":"next_before of the previous answer, for the next page."}}}`,
		Output: `{"type":"object","required":["mentions","unread","email"],"properties":{"unread":{"type":"integer","description":"How many are unread in all."},` +
			`"email":{"type":"boolean","description":"Whether you get them by email too (colliedesk_my_mention_email_set)."},"next_before":{"type":"string"},` +
			`"mentions":{"type":"array","items":{"type":"object","required":["id","conversation_id","note_id","asked_by","excerpt","created_at","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"conversation_id":{"type":"string"},"note_id":{"type":"string"},"display_ref":{"type":"string"},"asked_by":{"type":"string"},` +
			`"via_token":{"type":"string","description":"The agent token the note was written with, when one was."},"excerpt":{"type":"string","description":"The start of the note."},` +
			`"created_at":{"type":"string"},"read_at":{"type":"string"},"untrusted_customer_content":{"type":"object","properties":{"subject":{"type":"string"}}}}}}}}`,
		run: mentionsList,
	}
}

func mentionsMarkReadSpec() Spec {
	return Spec{
		Name:  "colliedesk_mentions_mark_read",
		Title: "Mark your mentions read or unread",
		Description: `Mark the mentions of the person your token acts as read once handled, or unread again with read false. ` +
			`Give exactly one of mention_ids (from colliedesk_mentions_list), conversation_id (every mention on that conversation's notes) or all true. Safe to repeat.`,
		Idempotent: true,
		REST:       []string{"POST /api/v1/mentions/read"},
		Input: `{"type":"object","additionalProperties":false,"minProperties":1,"properties":{"mention_ids":{"type":"array","minItems":1,"maxItems":100,"uniqueItems":true,"items":{"type":"string",` + uuid + `}},` +
			`"conversation_id":{"type":"string",` + uuid + `},"all":{"type":"boolean","enum":[true]},"read":{"type":"boolean","default":true,"description":"false marks them unread again."}}}`,
		Output: `{"type":"object","required":["changed","unread"],"properties":{"changed":{"type":"integer"},"unread":{"type":"integer","description":"How many stay unread."}}}`,
		run:    mentionsMarkRead,
	}
}

func myMentionEmailSetSpec() Spec {
	return Spec{
		Name: "colliedesk_my_mention_email_set", Title: "Turn your mention emails on or off",
		Description: `Turn the email that tells the person this token acts as about each note mentioning them on or off (on at first). ` +
			`Off, mentions still reach their Mentions list; colliedesk_mentions_list shows the setting as email.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/agents/me/mention-email"},
		Input:      `{"type":"object","additionalProperties":false,"required":["email"],"properties":{"email":{"type":"boolean"}}}`,
		Output:     `{"type":"object","required":["email"],"properties":{"email":{"type":"boolean"}}}`,
		run:        myMentionEmailSet,
	}
}
