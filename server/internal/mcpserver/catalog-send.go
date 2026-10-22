package mcpserver

// The tools below need the send tier: they reach a customer or change what the whole team works with.

const confirmFirst = `Before you call it, show the user what you will do and get their yes, unless they already told you to do exactly this. `

func replySendSpec() Spec {
	return Spec{
		Name:  "colliedesk_reply_send",
		Title: "Send a reply to the customer",
		Description: `Send a reply on a conversation. Colliedesk emails it to the customer (and to cc) through the workspace's email channel. ` +
			`It cannot be undone: an email that left cannot be called back. ` + confirmFirst +
			`Each call sends one more email, so never call it again to retry a call that succeeded; read the conversation first. ` +
			`To have a person check the words first, use colliedesk_reply_draft; with a token below send this tool files the same draft for a person to send. ` +
			`The reply is recorded as written by you through your token (written_by_agent_token). delivery says whether the email is queued. ` +
			`Pass seen_reply_number, the latest_reply_number of the colliedesk_conversation_get you read: when someone replied since, nothing is sent and the error shows their reply; read the conversation again before you send. ` +
			`When a team member has a reply to this customer open and not sent yet, the result carries a warning; say so to the user.` +
			"\n\nTier: send.",
		Tier: "send", Destructive: true,
		REST: []string{"POST /api/v1/conversations/{id}/messages", "GET /api/v1/conversations/{id}/messages", "GET /api/v1/agents", "GET /api/v1/inbox/activity"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","body","seen_reply_number"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"seen_reply_number":{"type":"integer","minimum":0,"description":"latest_reply_number from the colliedesk_conversation_get you based this reply on."},` +
			`"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The reply text (Markdown)."},` +
			`"cc":{"type":"array","maxItems":10,"items":{"type":"string","maxLength":254},"description":"Plain email addresses to copy, at most 10. The customer is always the recipient."}}}`,
		Output: `{"type":"object","required":["conversation_id","message"],"properties":{"conversation_id":{"type":"string"},"message":{"type":"object",` +
			`"required":["id","kind","created_at"],"properties":{"id":{"type":"string"},"kind":{"type":"string"},"created_at":{"type":"string"},"author":{"type":"string"},` +
			`"delivery":{"type":"string","description":"queued, sent or failed; absent when the reply is not emailed (no email channel)."},` +
			`"cc":{"type":"array","items":{"type":"string"}},` +
			`"written_by_agent_token":{"type":"object","required":["label","scope"],"properties":{"label":{"type":"string"},"scope":{"type":"string"}}}}},` +
			`"warning":{"type":"string","description":"A team member had a reply to this customer open and not sent yet."}}}`,
		run: replySend,
	}
}

func replyRetrySpec() Spec {
	return Spec{
		Name:  "colliedesk_reply_retry",
		Title: "Retry a failed reply",
		Description: `Send again a reply whose email delivery failed (delivery "failed" in colliedesk_conversation_get). It goes to the same recipients. ` +
			`It cannot be undone: the customer receives the email. ` + confirmFirst +
			`Only a failed delivery can be retried; a queued or sent one is refused, so a repeat never sends twice.` +
			"\n\nTier: send.",
		Tier: "send", Destructive: true,
		REST: []string{"POST /api/v1/conversations/{id}/messages/{messageId}/retry"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","message_id"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},"message_id":{"type":"string",` + uuid + `,"description":"The id of the failed reply."}}}`,
		Output: `{"type":"object","required":["conversation_id","message_id","delivery"],"properties":{"conversation_id":{"type":"string"},"message_id":{"type":"string"},"delivery":{"type":"string"}}}`,
		run:    replyRetry,
	}
}

func conversationCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversation_create",
		Title: "Open a conversation",
		Description: `Open a new conversation with a customer, with a subject and no messages yet. It sends nothing; write to the customer with colliedesk_reply_send afterwards, ` +
			`or open and email in one step with colliedesk_conversation_start. ` +
			`Find the customer with colliedesk_contacts_find, or add one with colliedesk_contact_create. ` +
			`Each call opens one more conversation, so do not repeat a call that succeeded. Close one you opened by mistake with colliedesk_conversation_update.` +
			"\n\nTier: send.",
		Tier: "send",
		REST: []string{"POST /api/v1/conversations"},
		Input: `{"type":"object","additionalProperties":false,"required":["contact_id","subject"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `},"subject":{"type":"string","minLength":1,"maxLength":500}}}`,
		Output: `{"type":"object","required":["conversation"],"properties":{"conversation":` + stateSchema + `}}`,
		run:    conversationCreate,
	}
}

func contactCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_contact_create",
		Title: "Add a customer",
		Description: `Add a customer (contact) with a name and an email address. It sends nothing. ` +
			`Look with colliedesk_contacts_find first, so the same person is not added twice.` +
			"\n\n" + untrustedNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"POST /api/v1/contacts"},
		Input: `{"type":"object","additionalProperties":false,"required":["name","email"],"properties":{` +
			`"name":{"type":"string","minLength":1,"maxLength":200},"email":{"type":"string","minLength":3,"maxLength":254}}}`,
		Output: `{"type":"object","required":["contact"],"properties":{"contact":` + contactRowSchema + `}}`,
		run:    contactCreate,
	}
}

func tagRenameSpec() Spec {
	return Spec{
		Name:  "colliedesk_tag_rename",
		Title: "Rename a tag",
		Description: `Rename a tag of the workspace. Every conversation with the tag shows the new name. ` +
			`Rename it back to undo. tag is matched like in colliedesk_conversation_update (exact spelling first, then the one tag that differs only in case).` +
			"\n\nTier: send.",
		Tier: "send",
		REST: []string{"PATCH /api/v1/tags/{id}", "GET /api/v1/tags"},
		Input: `{"type":"object","additionalProperties":false,"required":["tag","new_name"],"properties":{` +
			`"tag":{"type":"string","minLength":1,"maxLength":100,"description":"The current name."},"new_name":{"type":"string","minLength":1,"maxLength":100}}}`,
		Output: `{"type":"object","required":["tag"],"properties":{"tag":{"type":"object","required":["id","name"],"properties":{"id":{"type":"string"},"name":{"type":"string"}}}}}`,
		run:    tagRename,
	}
}

func tagDeleteSpec() Spec {
	return Spec{
		Name:  "colliedesk_tag_delete",
		Title: "Delete a tag",
		Description: `Delete a tag from the workspace and take it off every conversation that has it. ` +
			`It cannot be undone: a tag made again with the same name is a new, empty tag, and the conversations are not tagged again. ` + confirmFirst +
			`To take a tag off one conversation only, use colliedesk_conversation_update (remove_tags).` +
			"\n\nTier: send.",
		Tier: "send", Destructive: true,
		REST: []string{"DELETE /api/v1/tags/{id}", "GET /api/v1/tags"},
		Input: `{"type":"object","additionalProperties":false,"required":["tag"],"properties":{` +
			`"tag":{"type":"string","minLength":1,"maxLength":100,"description":"The name of the tag to delete."}}}`,
		Output: `{"type":"object","required":["deleted"],"properties":{"deleted":{"type":"object","required":["id","name"],"properties":{"id":{"type":"string"},"name":{"type":"string"}}}}}`,
		run:    tagDelete,
	}
}

func tokenRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_token_revoke",
		Title: "Revoke this token",
		Description: `Revoke the agent token this connection uses, so nobody can use it again. Every later call with it fails. ` +
			`It cannot be undone: a workspace admin must create a new token (Settings, Sessions and devices) to connect again. ` +
			`Use it when the user asks you to disconnect, or when you believe the token leaked. ` + confirmFirst +
			"\n\nTier: any (works with every token).",
		Tier: "any", Destructive: true,
		REST:   []string{"POST /api/v1/auth/logout"},
		Input:  noArguments,
		Output: `{"type":"object","required":["revoked","message"],"properties":{"revoked":{"type":"boolean"},"message":{"type":"string"}}}`,
		run:    tokenRevoke,
	}
}
