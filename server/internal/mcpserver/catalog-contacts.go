package mcpserver

// The tools below change customers (contacts) and start conversations by email; all need the send tier.

const contactMergeSchema = `{"type":"object","required":["merge_id","contact_id","merged_contact","conversations_moved","merged_at"],"properties":{` +
	`"merge_id":{"type":"string"},"contact_id":{"type":"string","description":"The contact that was kept."},` +
	`"merged_contact":` + contactRowSchema + `,"conversations_moved":{"type":"integer"},"merged_at":{"type":"string"},` +
	`"undone_at":{"type":["string","null"]}}}`

func contactUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_contact_update",
		Title: "Rename a customer",
		Description: `Change the name of a customer (contact). The team sees the new name, and later emails address the customer by it; the email address stays. ` +
			`Rename again to undo.` +
			"\n\n" + untrustedNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"PATCH /api/v1/contacts/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["contact_id","name"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `},"name":{"type":"string","minLength":1,"maxLength":200}}}`,
		Output: `{"type":"object","required":["contact"],"properties":{"contact":` + contactRowSchema + `}}`,
		run:    contactUpdate,
	}
}

func contactMergeSpec() Spec {
	return Spec{
		Name:  "colliedesk_contact_merge",
		Title: "Merge a duplicate customer",
		Description: `Merge a duplicate customer into the one to keep (contact_id): every conversation of the duplicate moves to it, mail from the duplicate's address joins it, ` +
			`and replies on the moved conversations go to the kept customer's email. Nothing is deleted. ` + confirmFirst +
			`Undo it with colliedesk_contact_unmerge and the merge_id. A customer merged already, or one others are merged into, cannot be merged; the error says which.` +
			"\n\n" + untrustedNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"POST /api/v1/contacts/{id}/merges"},
		Input: `{"type":"object","additionalProperties":false,"required":["contact_id","duplicate_contact_id"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `,"description":"The customer to keep."},` +
			`"duplicate_contact_id":{"type":"string",` + uuid + `,"description":"The duplicate to merge into it."}}}`,
		Output: `{"type":"object","required":["merge"],"properties":{"merge":` + contactMergeSchema + `}}`,
		run:    contactMerge,
	}
}

func contactUnmergeSpec() Spec {
	return Spec{
		Name:  "colliedesk_contact_unmerge",
		Title: "Undo a merge of customers",
		Description: `Undo a merge: the conversations it moved go back to the duplicate, which is a customer of its own again. ` +
			`Conversations that arrived after the merge stay with the kept customer. Find merge_id in colliedesk_contact_get (merges).` +
			"\n\n" + untrustedNotice + "\n\nTier: send.",
		Tier: "send",
		REST: []string{"DELETE /api/v1/contacts/{id}/merges/{mergeId}"},
		Input: `{"type":"object","additionalProperties":false,"required":["contact_id","merge_id"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `,"description":"The customer that was kept."},"merge_id":{"type":"string",` + uuid + `}}}`,
		Output: `{"type":"object","required":["merge"],"properties":{"merge":` + contactMergeSchema + `}}`,
		run:    contactUnmerge,
	}
}

func conversationStartSpec() Spec {
	return Spec{
		Name:  "colliedesk_conversation_start",
		Title: "Start a conversation by email",
		Description: `Open a new conversation with a customer and email them its first message (and cc) in one step. ` +
			`It cannot be undone: an email that left cannot be called back. ` + confirmFirst +
			`If the email is refused (no email channel, the channel is off, trial rules, cc rules) nothing is stored. The customer's answer joins the same conversation. ` +
			`A workspace with several email channels starts from channel_id, or from its default one. ` +
			`During the trial a new conversation reaches someone who never wrote in, so it is refused until the sending domain is verified or a plan is chosen. ` +
			`Each call opens one more conversation and sends one more email, so never repeat a call that succeeded. ` +
			`Give contact_id for a customer found with colliedesk_contacts_find, or new_contact to add the customer in the same step (an address that is already a customer is refused and the error names that customer). ` +
			`To open a conversation without emailing, use colliedesk_conversation_create.` +
			"\n\nTier: send.",
		Tier: "send", Destructive: true,
		REST: []string{"POST /api/v1/conversations", "GET /api/v1/conversations/{id}/messages"},
		Input: `{"type":"object","additionalProperties":false,"required":["subject","body"],"properties":{` +
			`"contact_id":{"type":"string",` + uuid + `,"description":"The customer; give this or new_contact."},` +
			`"new_contact":{"type":"object","additionalProperties":false,"required":["name","email"],"description":"A customer to add with the conversation; give this or contact_id.",` +
			`"properties":{"name":{"type":"string","minLength":1,"maxLength":200},"email":{"type":"string","minLength":3,"maxLength":254}}},` +
			`"subject":{"type":"string","minLength":1,"maxLength":500},` +
			`"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The first message (Markdown)."},` +
			`"cc":{"type":"array","maxItems":10,"items":{"type":"string","maxLength":254},"description":"Plain email addresses to copy, at most 10."},` +
			`"channel_id":{"type":"string",` + uuid + `,"description":"The email channel it starts from (colliedesk_channels_list), which its replies keep leaving from. Leave out for the default one."}}}`,
		Output: `{"type":"object","required":["conversation"],"properties":{"conversation":` + stateSchema + `,` +
			`"message":{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"delivery":{"type":"string","description":"queued, sent or failed."}}}}}`,
		run: conversationStart,
	}
}
