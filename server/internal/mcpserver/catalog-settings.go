package mcpserver

// The tools below read and change workspace settings: the workspace timezone, your own timezone and
// display name, the team, invites,
// sessions, the web form, the email channel and the plan. Reading needs read (invite links, tokens
// and the inbound secret are never in an answer). Changing needs the admin tier, which acts with the
// creator's admin role; a lower tier's call is filed as an agent request a person approves. Some
// changes are requests for every tier: creating an invite, any change to an admin or that makes one,
// and signing out another person.

const (
	belowAdmin = `With a token below admin, Colliedesk files the change as an agent request a person approves in Colliedesk (Requests); follow it with colliedesk_request_get.`

	memberProperty = `"member":{"type":"string","minLength":1,"maxLength":254,"description":"The team member's email or id (see colliedesk_agents_list)."}`

	inviteSchema = `{"type":"object","required":["id","email","role","state"],"properties":{"id":{"type":"string"},"email":{"type":"string"},` +
		`"role":{"type":"string","enum":["admin","agent"]},"state":{"type":"string","enum":["pending","accepted","revoked","expired"]},` +
		`"invited_by":{"type":"string"},"created_at":{"type":"string"},"expires_at":{"type":"string"}}}`

	sessionSchema = `{"type":"object","required":["id","label","owner","state"],"properties":{"id":{"type":"string"},"label":{"type":"string"},` +
		`"owner":{"type":"string","description":"Email of the team member it acts as"},"token_tier":{"type":["string","null"],"description":"read, draft, send or admin for an agent token, custom for one with its own permissions; null for a person's sign-in"},"permissions":{"type":"array","items":{"type":"string"},"description":"What an agent token holds (resource:action); empty for a person's sign-in"},` +
		`"this_token":{"type":"boolean"},"state":{"type":"string","enum":["active","revoked","expired"]},"last_seen_at":{"type":"string"},"expires_at":{"type":"string"}}}`

	// emailChannelInput names one email channel of several; without it a tool acts on the default one.
	emailChannelInput  = `{"type":"object","additionalProperties":false,"properties":{"channel_id":{"type":"string",` + uuid + `,"description":"One email channel of several, from colliedesk_channels_list. Leave out for the default one (is_default in colliedesk_channels_list)."}}}`
	emailChannelChoice = `A workspace may have several email channels: give channel_id for one of them, or leave it out for the default one (is_default).`

	webFormSchema = `{"type":"object","required":["enabled","allowed_origins","submit_path"],"properties":{"enabled":{"type":"boolean"},` +
		`"allowed_origins":{"type":"array","items":{"type":"string"}},"submit_path":{"type":"string"}}}`

	emailChannelSchema = `{"type":"object","required":["connected"],"properties":{"connected":{"type":"boolean"},"channel_id":{"type":"string"},"support_address":{"type":"string"},` +
		`"sender_name":{"type":"string","description":"The name customers see in the From line of every reply (with \" via Colliedesk\" while the domain is not verified)."},` +
		`"inbound_secret_set":{"type":"boolean","description":"The secret itself is never shown to an agent."},"inbound_url":{"type":"string"},"hosted_address":{"type":["string","null"]},` +
		`"domain":{"type":"string"},"domain_verified":{"type":"boolean"},"dns_record_name":{"type":"string"},"dns_record_value":{"type":"string"},` +
		`"smtp_configured":{"type":"boolean"},"sending_from":{"type":["string","null"]},` +
		`"gmail":{"type":"object","description":"Gmail connected for sending: replies go out from address through Gmail. status failed means replies fail until a person reconnects (colliedesk_gmail_connect_request).",` +
		`"required":["available","connected"],"properties":{"available":{"type":"boolean","description":"Whether this server can connect Gmail."},"connected":{"type":"boolean"},` +
		`"address":{"type":"string"},"status":{"type":"string","enum":["connected","failed"]},"error":{"type":["string","null"]}}}}}`
)

func workspaceSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_workspace_settings_get",
		Title: "Show the workspace settings",
		Description: `Show the workspace's name, address (slug) and timezone, and your own timezone (the team member this token acts as). ` +
			`Times in Colliedesk are stored in UTC; the timezone decides how they are shown and what "today" means.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/auth/me"},
		Input: noArguments,
		Output: `{"type":"object","required":["workspace","your_timezone"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}},` +
			`"your_timezone":{"type":"string"},"your_timezone_is_the_workspace_default":{"type":"boolean"}}}`,
		run: workspaceSettingsGet,
	}
}

func workspaceSettingsUpdateSpec() Spec {
	return Spec{
		Name:        "colliedesk_workspace_settings_update",
		Title:       "Change the workspace timezone",
		Description: `Set the workspace timezone (an IANA name such as Europe/Berlin). It changes how every team member without a timezone of their own sees times. It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier:        "admin", Idempotent: true,
		REST:   []string{"PATCH /api/v1/workspace/timezone"},
		Input:  `{"type":"object","additionalProperties":false,"required":["timezone"],"properties":{"timezone":{"type":"string","minLength":1,"maxLength":64}}}`,
		Output: `{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}}}}`,
		run:    workspaceSettingsUpdate,
	}
}

func workspaceRenameSpec() Spec {
	return Spec{
		Name:  "colliedesk_workspace_rename",
		Title: "Rename the workspace",
		Description: `Set the workspace name (2 to 60 characters, no line breaks). The team sees it in Colliedesk; customers see it as the help center title while the help center ` +
			`has no title of its own. Replies never carry it: each email channel sends with its own sender name (colliedesk_email_channel_update), and renaming the workspace ` +
			`does not change it. On Colliedesk Cloud a name with a well-known brand in it is refused. It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"PATCH /api/v1/workspace/name"},
		Input:  `{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":2,"maxLength":60,"description":"Spaces at the ends are removed."}}}`,
		Output: `{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}}}}`,
		run:    workspaceRename,
	}
}

func myTimezoneSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_timezone_set",
		Title: "Change your own timezone",
		Description: `Set the timezone of the team member this token acts as (an IANA name), or clear it with null to follow the workspace timezone. ` +
			`It changes only how times are shown to that person. It is safe to repeat. With a read token Colliedesk files it as an agent request.` + "\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"PATCH /api/v1/agents/me/timezone"},
		Input:  `{"type":"object","additionalProperties":false,"required":["timezone"],"properties":{"timezone":{"type":["string","null"],"maxLength":64,"description":"null follows the workspace timezone."}}}`,
		Output: `{"type":"object","required":["your_timezone"],"properties":{"your_timezone":{"type":["string","null"]}}}`,
		run:    myTimezoneSet,
	}
}

const displayNameProperty = `"display_name":{"type":"string","maxLength":80,"description":"Shown instead of the email address. Spaces at the ends are removed; an empty string clears it."}`

func myDisplayNameSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_display_name_set",
		Title: "Change your own display name",
		Description: `Set the display name of the team member this token acts as: the name the team sees instead of the email address in the team list, ` +
			`on assignments and on notes. An empty string clears it, and the email address is shown again. At most 80 characters, no line breaks. ` +
			`It is safe to repeat. With a read token Colliedesk files it as an agent request.` + "\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"PATCH /api/v1/agents/me/display-name"},
		Input:  `{"type":"object","additionalProperties":false,"required":["display_name"],"properties":{` + displayNameProperty + `}}`,
		Output: `{"type":"object","required":["your_display_name"],"properties":{"your_display_name":{"type":"string","description":"Empty when cleared."}}}`,
		run:    myDisplayNameSet,
	}
}

func teamMemberUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_team_member_update",
		Title: "Change a team member's name, role or access",
		Description: `Change a team member's display name (an empty string clears it), role (agent or admin), or deactivate or reactivate them. Deactivating ends their sessions and tokens and moves their open and pending ` +
			`conversations to reassign_to, or leaves them unassigned; reactivating lets them sign in again with a new link. The last admin cannot be demoted or deactivated. ` +
			`Renaming alone is not a change to the admin role. A change to an admin (demoting, deactivating, reactivating) or making someone an admin always waits for a person's approval, whatever your tier. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin",
		REST: []string{"PATCH /api/v1/agents/{id}", "GET /api/v1/agents"},
		Input: `{"type":"object","additionalProperties":false,"required":["member"],"properties":{` + memberProperty + `,` + displayNameProperty + `,` +
			`"role":{"type":"string","enum":["agent","admin"]},"active":{"type":"boolean"},` +
			`"reassign_to":{"type":"string","maxLength":254,"description":"With active false: the email or id of the member who takes over the open conversations."}}}`,
		Output: `{"type":"object","required":["member"],"properties":{"member":{"type":"object","properties":{"id":{"type":"string"},"email":{"type":"string"},"display_name":{"type":"string"},"role":{"type":"string"},"active":{"type":"boolean"}}}}}`,
		run:    teamMemberUpdate,
	}
}

func invitesListSpec() Spec {
	return Spec{
		Name:        "colliedesk_invites_list",
		Title:       "List invites",
		Description: `List the workspace's invites, newest first, with their state (pending, accepted, revoked, expired). The one-time link of an invite is never shown.` + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/auth/invites"},
		Input:  noArguments,
		Output: `{"type":"object","required":["invites"],"properties":{"invites":{"type":"array","items":` + inviteSchema + `}}}`,
		run:    invitesList,
	}
}

func inviteCreateSpec() Spec {
	return approvalSpec("colliedesk_invite_create", "Ask to invite a team member",
		`Ask to invite a person by email, with the agent role (default) or the admin role. On the hosted cloud a pending invite takes a seat. `+
			`The one-time invite link is a credential: the person who approves sees it once and shares it with the invitee; it is never shown to you.`,
		[]string{"POST /api/v1/auth/invites"},
		`{"type":"object","additionalProperties":false,"required":["email"],"properties":{"email":{"type":"string","minLength":3,"maxLength":254},`+
			`"role":{"type":"string","enum":["agent","admin"],"default":"agent"}}}`, inviteCreate)
}

func inviteRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_invite_revoke",
		Title: "Revoke an invite",
		Description: `Revoke a pending invite, so its link stops working and its seat is free. It cannot be undone: a new invite is needed. ` + confirmFirst + " " + belowAdmin +
			"\n\nTier: admin.",
		Tier: "admin", Destructive: true,
		REST:   []string{"DELETE /api/v1/auth/invites/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["invite_id"],"properties":{"invite_id":{"type":"string",` + uuid + `,"description":"From colliedesk_invites_list."}}}`,
		Output: `{"type":"object","required":["revoked"],"properties":{"revoked":{"type":"boolean"}}}`,
		run:    inviteRevoke,
	}
}

func sessionsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_sessions_list",
		Title: "List sessions and agent tokens",
		Description: `List sign-ins and agent tokens, most recently used first: whose they are, the token tier, and whether they are active. With the admin tier you see the whole workspace; ` +
			`otherwise only those of the person this token acts as. No token value is ever shown.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/auth/sessions"},
		Input:  noArguments,
		Output: `{"type":"object","required":["sessions"],"properties":{"sessions":{"type":"array","items":` + sessionSchema + `}}}`,
		run:    sessionsList,
	}
}

func sessionRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_session_revoke",
		Title: "Revoke a session or agent token",
		Description: `Sign out a session or revoke an agent token of the workspace, for example a lost laptop or a leaked token. It cannot be undone: the person signs in again, ` +
			`or an admin creates a new token. Signing out another person (not an agent token, not the person this token acts as) always waits for a person's approval. ` +
			`To revoke the token you use now, call colliedesk_token_revoke. ` + confirmFirst + " " + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Destructive: true,
		REST:   []string{"DELETE /api/v1/auth/sessions/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["session_id"],"properties":{"session_id":{"type":"string",` + uuid + `,"description":"From colliedesk_sessions_list."}}}`,
		Output: `{"type":"object","required":["revoked"],"properties":{"revoked":{"type":"boolean"}}}`,
		run:    sessionRevoke,
	}
}

func webFormGetSpec() Spec {
	return Spec{
		Name:        "colliedesk_web_form_get",
		Title:       "Show the web form settings",
		Description: `Show whether the website contact form is on, which website origins may post to it, and the path they post to.` + "\n\nTier: read.",
		Tier:        "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/channels/web-form"},
		Input:  noArguments,
		Output: `{"type":"object","required":["web_form"],"properties":{"web_form":` + webFormSchema + `}}`,
		run:    webFormGet,
	}
}

func webFormUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_web_form_update",
		Title: "Change the web form settings",
		Description: `Turn the website contact form on or off and set the exact origins (such as https://example.com) whose pages may post to it. ` +
			`allowed_origins replaces the whole list. It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PUT /api/v1/channels/web-form"},
		Input: `{"type":"object","additionalProperties":false,"required":["enabled","allowed_origins"],"properties":{"enabled":{"type":"boolean"},` +
			`"allowed_origins":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":200}}}}`,
		Output: `{"type":"object","required":["web_form"],"properties":{"web_form":` + webFormSchema + `}}`,
		run:    webFormUpdate,
	}
}

func emailChannelGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_channel_get",
		Title: "Show the email channel",
		Description: `Show an email channel: the support address, whether an inbound secret is set (never the secret), where to forward mail, ` +
			`the DNS record that verifies the sending domain, and the address replies are sent from. ` + emailChannelChoice + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/channels/email", "GET /api/v1/channels/{id}/email"},
		Input:  emailChannelInput,
		Output: `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:    emailChannelGet,
	}
}

func emailChannelUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_channel_update",
		Title: "Connect or change the email channel",
		Description: `Set the support address customers write to (a plain address such as support@example.com), the sender name customers see in the From line, or both. ` +
			`support_address connects the channel when it is not connected; a new domain needs verifying again (colliedesk_email_domain_verify). ` +
			`To rename the sender only, send sender_name alone: the address and its domain verification stay. A new channel starts with the workspace name as its sender name. ` +
			`On Colliedesk Cloud a name with a well-known brand in it is refused. It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST: []string{"PUT /api/v1/channels/email"},
		Input: `{"type":"object","additionalProperties":false,"minProperties":1,"properties":{"support_address":{"type":"string","minLength":3,"maxLength":254,` +
			`"description":"Required to connect the channel; leave it out to keep the current address."},` +
			`"sender_name":{"type":"string","minLength":1,"maxLength":100,"description":"Spaces at the ends are removed; no line breaks or other control characters. Leave it out to keep the current name."}}}`,
		Output: `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:    emailChannelUpdate,
	}
}

func emailDomainVerifySpec() Spec {
	return Spec{
		Name:  "colliedesk_email_domain_verify",
		Title: "Verify the email domain",
		Description: `Check the DNS TXT record that proves the support address's domain is yours, so replies can be sent from it. Add the record shown by colliedesk_email_channel_get first; ` +
			`when it is not there yet the result says which record to add. A domain verified once is verified for every email channel on it. ` + emailChannelChoice + ` It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"POST /api/v1/channels/email/domain-verification", "POST /api/v1/channels/{id}/email/domain-verification"},
		Input:  emailChannelInput,
		Output: `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:    emailDomainVerify,
	}
}

func billingGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_billing_get",
		Title: "Show the plan and billing status",
		Description: `Show the hosted cloud plan: trial end, whether the workspace is locked, the subscription status and period end, seats used and the limits. ` +
			`Only an admin may read it, so with a token below admin Colliedesk files it as an agent request. On a self-hosted server there is no plan. ` +
			`To buy a plan or open the billing portal, use colliedesk_billing_request.` + "\n\nTier: admin.",
		Tier: "admin", ReadOnly: true,
		REST:  []string{"GET /api/v1/billing"},
		Input: noArguments,
		Output: `{"type":"object","properties":{"billing":{"type":"object","properties":{"plan":{"type":"string"},"status":{"type":"string"},"locked":{"type":"boolean"},` +
			`"trial_ends_at":{"type":"string"},"current_period_end":{"type":["string","null"]},"cancel_at_period_end":{"type":"boolean"},"limits":{"type":"object"},"usage":{"type":"object"}}},` +
			`"agent_request":` + filedRequestSchema + `,"next":{"type":"string"}},"anyOf":[{"required":["billing"]},{"required":["agent_request","next"]}]}`,
		run: billingGet,
	}
}
