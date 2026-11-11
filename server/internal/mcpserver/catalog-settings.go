package mcpserver

// The tools below read and change workspace settings: the workspace timezone, your own timezone and
// display name, the team, invites, sessions, the web form, the email channel and the plan. Invite
// links, tokens and the inbound secret are never in a list; an invite's link and a new inbound secret
// come back once, to the caller that made them.

const (
	memberProperty = `"member":{"type":"string","minLength":1,"maxLength":254,"description":"The team member's email, display name or id (see colliedesk_agents_list)."}`

	inviteSchema = `{"type":"object","required":["id","email","roles","state"],"properties":{"id":{"type":"string"},"email":{"type":"string"},` +
		`"roles":{"type":"array","items":{"type":"string"},"description":"Names of the roles the person gets on joining."},"state":{"type":"string","enum":["pending","accepted","revoked","expired"]},` +
		`"invited_by":{"type":"string"},"created_at":{"type":"string"},"expires_at":{"type":"string"}}}`

	sessionSchema = `{"type":"object","required":["id","label","owner","state"],"properties":{"id":{"type":"string"},"label":{"type":"string"},` +
		`"owner":{"type":"string","description":"Email of the team member it acts as"},"owner_name":{"type":"string"},` +
		`"token":{"type":["object","null"],"description":"null for a person's sign-in. kind role (follows the role named in role), custom (a list picked when it was made) legacy (made before roles) or oauth (an AI app connected with OAuth: label is the app, it has what its person holds); given is what it was given; stopped says why a role token does nothing now.",` +
		`"properties":{"kind":{"type":"string","enum":["role","custom","legacy","oauth"]},"role":{"type":["string","null"]},"given":{"type":"array","items":{"type":"string"}},"legacy_tier":{"type":"string"},"stopped":{"type":"string"}}},` +
		`"permissions":{"type":"array","items":{"type":"string"},"description":"What an agent token was given (resource:action); empty for a person's sign-in"},` +
		`"this_token":{"type":"boolean"},"state":{"type":"string","enum":["active","revoked","expired"]},"last_seen_at":{"type":"string"},"expires_at":{"type":"string"},` +
		`"app":{"type":"object","description":"Only for an AI app connected with OAuth (label is its name): the browser it was allowed from, when, and whether it must sign in again (disconnected or expired).",` +
		`"properties":{"user_agent":{"type":"string"},"connected_at":{"type":"string"},"needs_reconnecting":{"type":"boolean"}}}}}`

	// emailChannelInput names one email channel of several; without it a tool acts on the default one.
	emailChannelInput  = `{"type":"object","additionalProperties":false,"properties":{"channel_id":{"type":"string",` + uuid + `,"description":"One email channel of several, from colliedesk_channels_list. Leave out for the default one (is_default in colliedesk_channels_list)."}}}`
	emailChannelChoice = `A workspace may have several email channels: give channel_id for one of them, or leave it out for the default one (is_default).`

	memberSchema = `{"type":"object","required":["id","email","display_name","roles","active"],"properties":{"id":{"type":"string"},"email":{"type":"string"},"display_name":{"type":"string"},` +
		`"roles":{"type":"array","items":{"type":"string"},"description":"Names of the roles they hold."},"active":{"type":"boolean"}}}`

	webFormSchema = `{"type":"object","required":["enabled","allowed_origins","submit_path"],"properties":{"enabled":{"type":"boolean"},` +
		`"allowed_origins":{"type":"array","items":{"type":"string"}},"submit_path":{"type":"string"}}}`

	emailChannelSchema = `{"type":"object","required":["connected"],"properties":{"connected":{"type":"boolean"},"channel_id":{"type":"string"},"support_address":{"type":"string"},` +
		`"sender_name":{"type":"string","description":"The name customers see in the From line of every reply (with \" via Colliedesk\" while the domain is not verified)."},` +
		`"inbound_secret_set":{"type":"boolean","description":"The secret itself is never shown to an agent."},"inbound_url":{"type":"string"},"hosted_address":{"type":["string","null"]},` +
		`"domain":{"type":"string"},"domain_verified":{"type":"boolean"},"dns_record_name":{"type":"string"},"dns_record_value":{"type":"string"},` +
		`"smtp_configured":{"type":"boolean"},"sending_from":{"type":["string","null"]},` +
		`"mail_through_mailbox":{"type":"boolean","description":"Whether customer mail has arrived through the support mailbox. Until it has, new customer mail may still arrive in the team's old help desk; a test message does not count. On Colliedesk Cloud a person moves the mail by forwarding the support mailbox to hosted_address. A mailbox cannot forward to inbound_url, an HTTP address: a self-hosted server needs a forwarder that posts each mail to inbound_url with the channel's inbound secret (docs/self-host/email.md)."},` +
		`"mail_switch":{"type":"object","required":["imported","mailboxes"],"description":"The workspace's support mailboxes whose customer mail does not arrive here yet, the same for every email channel; the inbox shows one line while mailboxes is not empty. A hosted forwarding address has no mailbox to move and is never listed.",` +
		`"properties":{"imported":{"type":"boolean","description":"Whether the workspace finished an import: then new mail to these mailboxes still arrives in the old help desk."},` +
		`"mailboxes":{"type":"array","items":{"type":"object","required":["channel_id","name","support_address"],"properties":{"channel_id":{"type":"string"},"name":{"type":"string"},"support_address":{"type":"string"}}}}}},` +
		`"answers_before_forwarding":{"type":"string","enum":["colliedesk","support_mailbox"],"description":"Where a customer's answer to a reply goes while mail_through_mailbox is false: colliedesk (it joins the conversation) or support_mailbox (this server cannot receive it; it reaches Colliedesk only once the mailbox forwards here)."},` +
		`"gmail":{"type":"object","description":"Gmail connected for sending: replies go out from address through Gmail. status failed means replies fail until a person reconnects (colliedesk_gmail_connect).",` +
		`"required":["available","connected"],"properties":{"available":{"type":"boolean","description":"Whether this server can connect Gmail."},"connected":{"type":"boolean"},` +
		`"address":{"type":"string"},"status":{"type":"string","enum":["connected","failed"]},"error":{"type":["string","null"]}}},` +
		`"microsoft":{"type":"object","description":"Microsoft 365 connected for sending: replies go out from address through Microsoft. A channel has one mailbox connection, Gmail or Microsoft 365. status failed means replies fail until a person reconnects (colliedesk_microsoft_connect); server_credentials_refused means replies wait for the server's administrator and reconnecting does not help.",` +
		`"required":["available","connected"],"properties":{"available":{"type":"boolean","description":"Whether this server can connect Microsoft 365."},"connected":{"type":"boolean"},` +
		`"address":{"type":"string"},"status":{"type":"string","enum":["connected","failed"]},"error":{"type":["string","null"]},` +
		`"admin_consent_url":{"type":["string","null"],"description":"Where an organization's Microsoft 365 admin approves Colliedesk when a person's sign-in needs it; give it to a person, never open it."},` +
		`"server_credentials_refused":{"type":"boolean"}}}}}`
)

func workspaceSettingsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_workspace_settings_get",
		Title: "Show the workspace settings",
		Description: `Show the workspace's name, address (slug) and timezone, and your own timezone (the team member this token acts as). ` +
			`Times in Colliedesk are stored in UTC; the timezone decides how they are shown and what "today" means.`,
		ReadOnly: true,
		REST:     []string{"GET /api/v1/auth/me"},
		Input:    noArguments,
		Output: `{"type":"object","required":["workspace","your_timezone"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}},` +
			`"your_timezone":{"type":"string"},"your_timezone_is_the_workspace_default":{"type":"boolean"}}}`,
		run: workspaceSettingsGet,
	}
}

func workspaceSettingsUpdateSpec() Spec {
	return Spec{
		Name:        "colliedesk_workspace_settings_update",
		Title:       "Change the workspace timezone",
		Description: `Set the workspace timezone (an IANA name such as Europe/Berlin). It changes how every team member without a timezone of their own sees times. It is safe to repeat.`,
		Idempotent:  true,
		REST:        []string{"PATCH /api/v1/workspace/timezone"},
		Input:       `{"type":"object","additionalProperties":false,"required":["timezone"],"properties":{"timezone":{"type":"string","minLength":1,"maxLength":64}}}`,
		Output:      `{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}}}}`,
		run:         workspaceSettingsUpdate,
	}
}

func workspaceRenameSpec() Spec {
	return Spec{
		Name:  "colliedesk_workspace_rename",
		Title: "Rename the workspace",
		Description: `Set the workspace name (2 to 60 characters, no line breaks). The team sees it in Colliedesk; customers see it as the help center title while the help center ` +
			`has no title of its own. Replies never carry it: each email channel sends with its own sender name (colliedesk_email_channel_update), and renaming the workspace ` +
			`does not change it. On Colliedesk Cloud a name with a well-known brand in it is refused. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/workspace/name"},
		Input:      `{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":2,"maxLength":60,"description":"Spaces at the ends are removed."}}}`,
		Output:     `{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"},"timezone":{"type":"string"}}}}}`,
		run:        workspaceRename,
	}
}

func myTimezoneSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_timezone_set",
		Title: "Change your own timezone",
		Description: `Set the timezone of the team member this token acts as (an IANA name), or clear it with null to follow the workspace timezone. ` +
			`It changes only how times are shown to that person. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/agents/me/timezone"},
		Input:      `{"type":"object","additionalProperties":false,"required":["timezone"],"properties":{"timezone":{"type":["string","null"],"maxLength":64,"description":"null follows the workspace timezone."}}}`,
		Output:     `{"type":"object","required":["your_timezone"],"properties":{"your_timezone":{"type":["string","null"]}}}`,
		run:        myTimezoneSet,
	}
}

const displayNameProperty = `"display_name":{"type":"string","maxLength":80,"description":"Shown instead of the email address. Spaces at the ends are removed; an empty string clears it."}`

func myDisplayNameSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_my_display_name_set",
		Title: "Change your own display name",
		Description: `Set the display name of the team member this token acts as: the name the team sees instead of the email address in the team list, ` +
			`on assignments and on notes. An empty string clears it, and the email address is shown again. At most 80 characters, no line breaks. ` +
			`It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/agents/me/display-name"},
		Input:      `{"type":"object","additionalProperties":false,"required":["display_name"],"properties":{` + displayNameProperty + `}}`,
		Output:     `{"type":"object","required":["your_display_name"],"properties":{"your_display_name":{"type":"string","description":"Empty when cleared."}}}`,
		run:        myDisplayNameSet,
	}
}

func teamMemberUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_team_member_update",
		Title: "Change a team member's name or access",
		Description: `Change a team member's display name (an empty string clears it), or deactivate or reactivate them. Deactivating ends their sessions and tokens and moves their open and pending ` +
			`conversations to reassign_to, or leaves them unassigned; reactivating lets them sign in again with a new link and gives back the roles they held. ` +
			`The last active member holding the Admin role cannot be deactivated, and you cannot change someone who holds a permission you lack. ` +
			`To change their roles, use colliedesk_member_roles_set.`,
		REST: []string{"PATCH /api/v1/agents/{id}", "GET /api/v1/agents"},
		Input: `{"type":"object","additionalProperties":false,"required":["member"],"properties":{` + memberProperty + `,` + displayNameProperty + `,` +
			`"active":{"type":"boolean"},` +
			`"reassign_to":{"type":"string","maxLength":254,"description":"With active false: the email, display name or id of the member who takes over the open conversations."}}}`,
		Output: `{"type":"object","required":["member"],"properties":{"member":` + memberSchema + `}}`,
		run:    teamMemberUpdate,
	}
}

func invitesListSpec() Spec {
	return Spec{
		Name:        "colliedesk_invites_list",
		Title:       "List invites",
		Description: `List the workspace's invites, newest first, with their state (pending, accepted, revoked, expired). The one-time link of an invite is never shown.`,
		ReadOnly:    true,
		REST:        []string{"GET /api/v1/auth/invites"},
		Input:       noArguments,
		Output:      `{"type":"object","required":["invites"],"properties":{"invites":{"type":"array","items":` + inviteSchema + `}}}`,
		run:         invitesList,
	}
}

func inviteCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_invite_create",
		Title: "Invite a team member",
		Description: `Invite a person by email with one or more roles by name (the Agent role when you give none); they get those roles on joining. ` +
			`Someone already in the workspace cannot be invited this way (already_a_member): change their roles with colliedesk_member_roles_set. ` +
			`You can only give roles whose every permission you hold. On the hosted cloud a pending invite takes a seat. ` +
			`The answer holds the one-time sign-in link, shown this once: it is a credential, so give it only to the invited person, privately. Revoke it with colliedesk_invite_revoke.`,
		REST: []string{"POST /api/v1/auth/invites"},
		Input: `{"type":"object","additionalProperties":false,"required":["email"],"properties":{"email":{"type":"string","minLength":3,"maxLength":254},` +
			`"roles":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"string","minLength":1,"maxLength":60},"description":"Role names (colliedesk_roles_list), any case. Leave out for Agent."}}}`,
		Output: `{"type":"object","required":["invite","url","next"],"properties":{"invite":` + inviteSchema + `,"url":{"type":"string"},"next":{"type":"string"}}}`,
		run:    inviteCreate,
	}
}

func inviteRevokeSpec() Spec {
	return Spec{
		Name:        "colliedesk_invite_revoke",
		Title:       "Revoke an invite",
		Description: `Revoke a pending invite, so its link stops working and its seat is free. It cannot be undone: a new invite is needed. ` + confirmFirst,
		Destructive: true,
		REST:        []string{"DELETE /api/v1/auth/invites/{id}"},
		Input:       `{"type":"object","additionalProperties":false,"required":["invite_id"],"properties":{"invite_id":{"type":"string",` + uuid + `,"description":"From colliedesk_invites_list."}}}`,
		Output:      `{"type":"object","required":["revoked"],"properties":{"revoked":{"type":"boolean"}}}`,
		run:         inviteRevoke,
	}
}

func sessionsListSpec() Spec {
	return Spec{
		Name:  "colliedesk_sessions_list",
		Title: "List sessions and agent tokens",
		Description: `List sign-ins and agent tokens, most recently used first: whose they are, how each token got its permissions (a role it follows, or a list picked when it was made), and whether they are active. ` +
			`With team:revoke you see the whole workspace; otherwise only those of the person this token acts as. No token value is ever shown.`,
		ReadOnly: true,
		REST:     []string{"GET /api/v1/auth/sessions"},
		Input:    noArguments,
		Output:   `{"type":"object","required":["sessions"],"properties":{"sessions":{"type":"array","items":` + sessionSchema + `}}}`,
		run:      sessionsList,
	}
}

func sessionRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_session_revoke",
		Title: "Revoke a session or agent token",
		Description: `Sign out a session or revoke an agent token of the workspace, for example a lost laptop or a leaked token. It cannot be undone: the person signs in again, ` +
			`or makes a new token. Your own person's sessions and tokens need nothing more; another person's need team:revoke, and you cannot sign out someone who holds a permission you lack. ` +
			`To revoke the token you use now, call colliedesk_token_revoke. ` + confirmFirst,
		Destructive: true,
		REST:        []string{"DELETE /api/v1/auth/sessions/{id}"},
		Input:       `{"type":"object","additionalProperties":false,"required":["session_id"],"properties":{"session_id":{"type":"string",` + uuid + `,"description":"From colliedesk_sessions_list."}}}`,
		Output:      `{"type":"object","required":["revoked"],"properties":{"revoked":{"type":"boolean"}}}`,
		run:         sessionRevoke,
	}
}

func webFormGetSpec() Spec {
	return Spec{
		Name:        "colliedesk_web_form_get",
		Title:       "Show the web form settings",
		Description: `Show whether the website contact form is on, which website origins may post to it, and the path they post to.`,
		ReadOnly:    true,
		REST:        []string{"GET /api/v1/channels/web-form"},
		Input:       noArguments,
		Output:      `{"type":"object","required":["web_form"],"properties":{"web_form":` + webFormSchema + `}}`,
		run:         webFormGet,
	}
}

func webFormUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_web_form_update",
		Title: "Change the web form settings",
		Description: `Turn the website contact form on or off and set the exact origins (such as https://example.com) whose pages may post to it. ` +
			`allowed_origins replaces the whole list. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/channels/web-form"},
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
			`the DNS record that verifies the sending domain, and the address replies are sent from. ` + emailChannelChoice,
		ReadOnly: true,
		REST:     []string{"GET /api/v1/channels/email", "GET /api/v1/channels/{id}/email"},
		Input:    emailChannelInput,
		Output:   `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:      emailChannelGet,
	}
}

func emailChannelUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_channel_update",
		Title: "Connect or change the email channel",
		Description: `Set the support address customers write to (a plain address such as support@example.com), the sender name customers see in the From line, or both. ` +
			`support_address connects the channel when it is not connected; a new domain needs verifying again (colliedesk_email_domain_verify). ` +
			`To rename the sender only, send sender_name alone: the address and its domain verification stay. A new channel starts with the workspace name as its sender name. ` +
			`On Colliedesk Cloud a name with a well-known brand in it is refused. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/channels/email"},
		Input: `{"type":"object","additionalProperties":false,"minProperties":1,"properties":{"support_address":{"type":"string","minLength":3,"maxLength":254,` +
			`"description":"Required to connect the channel; leave it out to keep the current address."},` +
			`"sender_name":{"type":"string","minLength":1,"maxLength":100,"description":"Spaces at the ends are removed; no line breaks or other control characters. Leave it out to keep the current name."}}}`,
		Output: `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:    emailChannelUpdate,
	}
}

func emailTestMessageSendSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_test_message_send",
		Title: "Send yourself a test email",
		Description: `Deliver one test email from your own agent's address to an email channel, the way a customer's mail arrives: it opens a conversation ` +
			`"Test message from Colliedesk" in the inbox. Answer it with colliedesk_reply_send to see the whole loop; the reply is emailed to your agent's address ` +
			`(allowed on a trial, because it reaches only you; a Cc to anyone else is refused). Five per workspace an hour. ` + emailChannelChoice,

		REST:   []string{"POST /api/v1/channels/email/test-message", "POST /api/v1/channels/{id}/email/test-message"},
		Input:  emailChannelInput,
		Output: `{"type":"object","required":["conversation_id","message_id","sent_to"],"properties":{"conversation_id":{"type":"string"},"message_id":{"type":"string"},"sent_to":{"type":"string"}}}`,
		run:    emailTestMessageSend,
	}
}

func emailForwardingConfirmationGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_forwarding_confirmation_get",
		Title: "Read Gmail's forwarding confirmation",
		Description: `When a Gmail or Google Workspace mailbox is set to forward to an email channel, Google emails a confirmation code and link to it. ` +
			`This reads the newest one that reached the channel in the last 24 hours: code, confirm_url, received_at and the conversation it opened. ` +
			`confirm_url is null unless the email passed the trusted sender check and the link is on Google's own host; give the code or link to the person who set up forwarding, never open it yourself. ` +
			`Not found when none arrived. It reads only.`,
		ReadOnly: true,
		REST:     []string{"GET /api/v1/channels/{id}/email/forwarding-confirmation"},
		Input:    `{"type":"object","additionalProperties":false,"required":["channel_id"],"properties":{"channel_id":{"type":"string",` + uuid + `,"description":"The email channel, from colliedesk_channels_list."}}}`,
		Output: `{"type":"object","required":["code","confirm_url","received_at","conversation_id"],"properties":{"code":{"type":"string"},` +
			`"confirm_url":{"type":["string","null"]},"received_at":{"type":"string"},"conversation_id":{"type":"string"}}}`,
		run: emailForwardingConfirmationGet,
	}
}

func emailDomainVerifySpec() Spec {
	return Spec{
		Name:  "colliedesk_email_domain_verify",
		Title: "Verify the email domain",
		Description: `Check the DNS TXT record that proves the support address's domain is yours, so replies can be sent from it. Add the record shown by colliedesk_email_channel_get first; ` +
			`when it is not there yet the result says which record to add. A domain verified once is verified for every email channel on it. ` + emailChannelChoice + ` It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"POST /api/v1/channels/email/domain-verification", "POST /api/v1/channels/{id}/email/domain-verification"},
		Input:      emailChannelInput,
		Output:     `{"type":"object","required":["email_channel"],"properties":{"email_channel":` + emailChannelSchema + `}}`,
		run:        emailDomainVerify,
	}
}

func billingGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_billing_get",
		Title: "Show the plan and billing status",
		Description: `Show the hosted cloud plan: trial end, whether the workspace is locked, the subscription status and period end, seats used and the limits. ` +
			`On a self-hosted server there is no plan. ` +
			`To buy a plan, open the billing portal or ask for a Freshdesk migration, use colliedesk_billing_action. migration_requested_at is when someone last asked for one (null when none did).`,
		ReadOnly: true,
		REST:     []string{"GET /api/v1/billing"},
		Input:    noArguments,
		Output: `{"type":"object","properties":{"billing":{"type":"object","properties":{"plan":{"type":"string"},"status":{"type":"string"},"locked":{"type":"boolean"},` +
			`"trial_ends_at":{"type":"string"},"current_period_end":{"type":["string","null"]},"cancel_at_period_end":{"type":"boolean"},"migration_requested_at":{"type":["string","null"]},"limits":{"type":"object"},"usage":{"type":"object"}}}},"required":["billing"]}`,
		run: billingGet,
	}
}
