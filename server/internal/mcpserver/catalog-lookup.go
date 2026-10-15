package mcpserver

func whoamiSpec() Spec {
	return Spec{
		Name:  "colliedesk_whoami",
		Title: "Show who this token acts as",
		Description: `Show the workspace and the team member this token acts as, the token's tier (read, draft or send), ` +
			`whether the workspace is locked, and which tools that tier can use. Call it first, and again when a tool says it needs a higher tier.` +
			"\n\nTier: any (works with every token).",
		Tier: "any", ReadOnly: true,
		REST:   []string{"GET /api/v1/auth/me"},
		Input:  noArguments,
		Output: `{"type":"object","required":["workspace","agent","token_tier","workspace_locked","tools_you_can_use"],"properties":{"workspace":{"type":"object"},"agent":{"type":"object"},"token_tier":{"type":"string","enum":["read","draft","send"]},"tier_meaning":{"type":"string"},"workspace_locked":{"type":"boolean"},"tools_you_can_use":{"type":"array","items":{"type":"string"}}}}`,
		run:    whoami,
	}
}

func contactsFindSpec() Spec {
	return Spec{
		Name:  "colliedesk_contacts_find",
		Title: "Find customers",
		Description: `Find customers (contacts) whose name or email contains your words. Returns up to limit matches, ordered by name, and says in truncated whether more matched. ` +
			`The query needs at least 2 characters.` +
			"\n\n" + untrustedNotice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST: []string{"GET /api/v1/contacts"},
		Input: `{"type":"object","additionalProperties":false,"required":["query"],"properties":{` +
			`"query":{"type":"string","minLength":2,"maxLength":200,"description":"Part of a name or an email address."},` +
			`"limit":{"type":"integer","minimum":1,"maximum":50,"default":10},` + detailProperty + `}}`,
		Output: `{"type":"object","required":["contacts","truncated"],"properties":{"contacts":{"type":"array","items":{"type":"object","required":["id","untrusted_customer_content"],"properties":{` +
			`"id":{"type":"string"},"created_at":{"type":"string"},"untrusted_customer_content":{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"},"organization":{"type":["string","null"]}}}}}},` +
			`"truncated":{"type":"boolean","description":"True when more contacts matched than were returned; narrow the query."}}}`,
		run: contactsFind,
	}
}

func tagsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_tags_list",
		Title: "List tags",
		Description: `List the tags of this workspace. Use these exact names in colliedesk_inbox_search (tag) and colliedesk_conversation_update (add_tags, remove_tags). ` +
			`Look here before you add a tag: colliedesk_conversation_update creates a tag only when no tag has the name, so reuse an existing one.` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/tags"},
		Input:  noArguments,
		Output: `{"type":"object","required":["tags","count"],"properties":{"tags":{"type":"array","items":{"type":"object","required":["id","name"],"properties":{"id":{"type":"string"},"name":{"type":"string"}}}},"count":{"type":"integer"}}}`,
		run:    tagsList,
	}
}

func agentsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_agents_list",
		Title: "List team members",
		Description: `List the people on the support team who can own a conversation, with the name and email to use in colliedesk_conversation_update (assignee).` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/agents"},
		Input:  noArguments,
		Output: `{"type":"object","required":["agents","count"],"properties":{"agents":{"type":"array","items":{"type":"object","required":["id","name","email","role"],"properties":{"id":{"type":"string"},"name":{"type":"string"},"email":{"type":"string"},"role":{"type":"string"}}}},"count":{"type":"integer"}}}`,
		run:    agentsList,
	}
}

func docsSearchSpec() Spec {
	return Spec{
		Name:  "colliedesk_docs_search",
		Title: "Search the user guides",
		Description: `Search the Colliedesk user guides built into this server and return the best matching sections with an excerpt. ` +
			`Use it when you need to know how a feature works or what a setting does. It reads no workspace data and needs no network.` +
			"\n\nTier: any (works with every token).",
		Tier: "any", ReadOnly: true,
		Input: `{"type":"object","additionalProperties":false,"required":["query"],"properties":{` +
			`"query":{"type":"string","minLength":1,"maxLength":200,"description":"Words to look for, for example: revoke an invite."},` +
			`"limit":{"type":"integer","minimum":1,"maximum":10,"default":5}}}`,
		Output: `{"type":"object","required":["results","count"],"properties":{"results":{"type":"array","items":{"type":"object","required":["page","title","section","excerpt"],"properties":{` +
			`"page":{"type":"string","description":"The guide, for example invites."},"title":{"type":"string"},"section":{"type":"string"},"excerpt":{"type":"string"}}}},` +
			`"count":{"type":"integer"},"hint":{"type":"string"}}}`,
		run: docsSearch,
	}
}
