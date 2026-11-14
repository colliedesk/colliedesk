package mcpserver

// The tools below change what reaches outside the workspace or the whole workspace: export, billing,
// the email channel's credentials, the workspace address, numbering and response targets. A token
// that holds the permission acts at once; one that lacks it is refused with the permission it lacks.
// A result a person must open (a payment page, Google's or Microsoft's consent page) comes back as an
// address for the person behind the token: only they can finish it, in their own browser.

import (
	"net/http"
)

const (
	// doneSchema is the answer of a change Colliedesk confirms without a body.
	doneSchema = `{"type":"object","required":["done","next"],"properties":{"done":{"type":"boolean"},"next":{"type":"string"}}}`
	// consentSchema is the answer of a connection only the token's person can finish in a browser.
	consentSchema = `{"type":"object","required":["url","next"],"properties":{"url":{"type":"string","description":"The provider's page, for the person this token acts as."},"next":{"type":"string"}}}`

	responseTargetsSchema = `{"type":"object","required":["enabled","hours","targets"],"properties":{"enabled":{"type":"boolean"},"timezone":{"type":["string","null"]},"hours":{"type":"array"},"holidays":{"type":"array"},"targets":{"type":"object"},"warn_before_minutes":{"type":"integer"},"channel_ids":{"type":["array","null"]},"alert_email":{"type":"boolean"},"enabled_at":{"type":["string","null"]},"updated_at":{"type":["string","null"]}}}`
)

func replyDraftSpec() Spec {
	return Spec{
		Name:  "colliedesk_reply_draft",
		Title: "Draft a reply for a person to send",
		Description: `Leave a reply draft on a conversation. Nothing reaches the customer: the draft shows above the reply box with your token's name, ` +
			`and a person edits it, sends it or discards it. Use it when a person should check the words before they go out, or when you may not send. ` +
			`A new draft shows on top of the one this token left there before, which stays as an earlier draft: colliedesk_ai_drafts_list shows them and colliedesk_reply_draft_restore brings one back. ` +
			`files leaves up to 10 files, ` + inlineFilesLimitWords + ` in total per call (base64), with the draft: only the team sees them, and they reach the customer only when a person sends the draft with them. ` +
			`Without messages:send, cc may name only people already on the conversation.`,
		REST: []string{"POST /api/v1/conversations/{id}/reply-drafts"},
		Input: `{"type":"object","additionalProperties":false,"required":["conversation_id","body"],"properties":{` +
			`"conversation_id":{"type":"string",` + uuid + `},` +
			`"body":{"type":"string","minLength":1,"maxLength":20000,"description":"The reply text (Markdown)."},` +
			`"cc":{"type":"array","maxItems":10,"items":{"type":"string","maxLength":254},"description":"Plain email addresses to copy when it is sent."},` + filesProperty + `}}`,
		Output: `{"type":"object","required":["draft","next"],"properties":{"draft":{"type":"object","required":["id","conversation_id","status","files"],"properties":{` +
			`"id":{"type":"string"},"conversation_id":{"type":"string"},"status":{"type":"string"},"files":` + draftFilesSchema + `}},"next":{"type":"string"}}}`,
		run: replyDraft,
	}
}

func replyDraftRestoreSpec() Spec {
	return Spec{
		Name:  "colliedesk_reply_draft_restore",
		Title: "Bring an earlier reply draft back",
		Description: `Bring an earlier written reply draft back above the reply box, with its text, copies and files: one that a newer draft replaced (outcome superseded), ` +
			`that expired, or that a person discarded (find them with colliedesk_ai_drafts_list). The draft of the same author that waited in its place becomes the earlier one; ` +
			`nothing reaches the customer. A draft that was sent, a draft the built-in AI wrote, or one whose text was erased (90 days after it ended) cannot come back: write a new one with colliedesk_reply_draft.`,
		Idempotent: true,
		REST:       []string{"POST /api/v1/conversations/{id}/ai-drafts/{draftId}/restore"},
		Input:      `{"type":"object","additionalProperties":false,"required":["conversation_id","draft_id"],"properties":{` + aiDraftConversationProperty + `,` + aiDraftIDProperty + `}}`,
		Output:     `{"type":"object","required":["draft","next"],"properties":{"draft":` + aiDraftSchema + `,"next":{"type":"string"}}}`,
		run:        replyDraftRestore,
	}
}

func workspaceExportSpec() Spec {
	return Spec{
		Name:  "colliedesk_workspace_export",
		Title: "Export the workspace",
		Description: `Export all workspace data (a ZIP with every conversation, message, contact and attachment). delivery link (the default) answers a one-time download link ` +
			`that expires (expires_in_seconds): give it to the person you work for. delivery download answers the ZIP itself in data_base64, only while it is at most ` + inlineFilesLimitWords +
			`; a larger export is refused and needs the link. The whole data of the workspace leaves with it.`,
		REST:  []string{"POST /api/v1/export/link", "GET /api/v1/export"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"delivery":{"type":"string","enum":["link","download"],"default":"link"}}}`,
		Output: `{"type":"object","properties":{"url":{"type":"string"},"expires_in_seconds":{"type":"integer"},"filename":{"type":"string"},"size":{"type":"integer"},` +
			`"data_base64":{"type":"string"},"next":{"type":"string"}}}`,
		run: workspaceExport,
	}
}

func billingActionSpec() Spec {
	return Spec{
		Name:  "colliedesk_billing_action",
		Title: "Start a checkout, open billing or request a Freshdesk migration",
		Description: `On the hosted cloud: start a plan checkout (action checkout, with a plan) or open the billing portal (action portal); either answers a payment page address ` +
			`for the person you work for to open and pay. You never pay. Or request a Freshdesk data migration (action migration_request, with freshdesk_address and optionally times): ` +
			`it emails Colliedesk support with the workspace and your person's address, and Plan and billing then shows when it was sent (migration_requested_at of colliedesk_billing_get).`,
		REST: []string{"POST /api/v1/billing/checkout", "POST /api/v1/billing/portal", "POST /api/v1/billing/migration-request"},
		Input: `{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string","enum":["checkout","portal","migration_request"]},` +
			`"plan":{"type":"string","enum":["solo_monthly","solo_yearly","small_monthly","small_yearly","team_monthly","team_yearly","team25_monthly","team25_yearly","founding"],"description":"For checkout: the plan to buy."},` +
			`"freshdesk_address":{"type":"string","maxLength":253,"description":"For migration_request: the Freshdesk address, like yourcompany.freshdesk.com."},` +
			`"times":{"type":"string","maxLength":500,"description":"For migration_request: times that suit your person for a call, with a time zone. Optional."}}}`,
		Output: `{"type":"object","required":["next"],"properties":{"url":{"type":"string","description":"The payment page, for checkout and portal."},"next":{"type":"string"}}}`,
		run:    billingAction,
	}
}

func trialOfferAcceptSpec() Spec {
	return Spec{
		Name:  "colliedesk_trial_offer_accept",
		Title: "Accept a trial offer link",
		Description: `When the person you work for got a trial offer link from Colliedesk (an address with /trial-offer?token=), accept it here: the workspace's trial then ends 14 days from now, ` +
			`and a locked workspace opens at once. A trial end already later than that is kept. Pass the whole link or only its token. preview true only reads the offer ` +
			`(workspace, state, the trial end it would give, who can accept) and changes nothing. A spent, expired or withdrawn link, a workspace with a plan, or another workspace's link ` +
			`is refused with the reason and the trial end; for more time, the person writes to support@colliedesk.com.`,
		REST: []string{"GET /api/v1/trial-offer", "POST /api/v1/trial-offer/accept"},
		Input: `{"type":"object","additionalProperties":false,"required":["link"],"properties":{` +
			`"link":{"type":"string","maxLength":2000,"description":"The trial offer link, or only its token."},` +
			`"preview":{"type":"boolean","default":false,"description":"true: read the offer without accepting it."}}}`,
		Output: `{"type":"object","required":["offer","next"],"properties":{"offer":{"type":"object"},"previous_trial_ends_at":{"type":"string"},` +
			`"trial_ends_at":{"type":"string"},"extended":{"type":"boolean"},"next":{"type":"string"}}}`,
		run: trialOfferAccept,
	}
}

func emailChannelDisconnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_channel_disconnect",
		Title: "Disconnect the email channel",
		Description: `Disconnect the workspace's default email channel (is_default): its mail stops coming in and its replies stop going out until it is connected again; ` +
			`its conversations and messages stay. Its inbound secret stops working, which cannot be undone: connecting again gives a new one. Mail it held while off must be opened or discarded first. To remove another channel, use colliedesk_channels_delete.`,
		REST:   []string{"DELETE /api/v1/channels/email"},
		Input:  noArguments,
		Output: doneSchema,
		run:    emailChannelDisconnect,
	}
}

func gmailConnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_gmail_connect",
		Title: "Connect Gmail for sending",
		Description: `Start connecting a Gmail account to an email channel, so its replies are sent from that address through Gmail and kept in its Sent folder (incoming mail still arrives by forwarding). ` +
			`It answers Google's consent address: only the person this token acts as can finish it, by opening it within 10 minutes in a browser signed in to Colliedesk and agreeing on Google's page. ` +
			`Connecting again replaces the account. Needs the email channel and the server's Google settings (see colliedesk_email_channel_get, gmail). ` + emailChannelChoice,
		REST:   []string{"POST /api/v1/channels/email/gmail", "POST /api/v1/channels/{id}/email/gmail"},
		Input:  emailChannelInput,
		Output: consentSchema,
		run:    gmailConnect,
	}
}

func gmailDisconnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_gmail_disconnect",
		Title: "Disconnect Gmail",
		Description: `Disconnect the Gmail account of an email channel: Colliedesk's access is revoked at Google (unless another channel uses the same account) and its replies go out the way ` +
			`they did before Gmail was connected. A reply still waiting to be sent through Gmail fails and can be retried. The revoked access cannot be undone: connect again with colliedesk_gmail_connect. ` + emailChannelChoice,
		REST:   []string{"DELETE /api/v1/channels/email/gmail", "DELETE /api/v1/channels/{id}/email/gmail"},
		Input:  emailChannelInput,
		Output: doneSchema,
		run:    gmailDisconnect,
	}
}

func microsoftConnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_microsoft_connect",
		Title: "Connect Microsoft 365 for sending",
		Description: `Start connecting a Microsoft 365 (work or school) or Outlook.com (personal) mailbox to an email channel, so its replies are sent from that address through Microsoft ` +
			`and kept in its Sent Items (incoming mail still arrives by forwarding). It answers Microsoft's sign-in address: only the person this token acts as can finish it, ` +
			`by opening it within 10 minutes in a browser signed in to Colliedesk and signing in there. The mailbox must be the channel's support address; aliases and shared mailboxes are not supported yet. ` +
			`A work account may first need its organization's Microsoft 365 admin to approve Colliedesk (microsoft.admin_consent_url of colliedesk_email_channel_get). ` +
			`Connecting replaces the channel's Gmail or earlier Microsoft 365 connection. Needs the email channel and the server's Microsoft settings (see colliedesk_email_channel_get, microsoft). ` + emailChannelChoice,
		REST:   []string{"POST /api/v1/channels/email/microsoft", "POST /api/v1/channels/{id}/email/microsoft"},
		Input:  emailChannelInput,
		Output: consentSchema,
		run:    microsoftConnect,
	}
}

func microsoftDisconnectSpec() Spec {
	return Spec{
		Name:  "colliedesk_microsoft_disconnect",
		Title: "Disconnect Microsoft 365",
		Description: `Disconnect the Microsoft 365 mailbox of an email channel: Colliedesk deletes its access, and its replies go out the way they did before Microsoft 365 was connected. ` +
			`Microsoft cannot be asked to revoke it, so Colliedesk stays listed in the account's apps until the person removes it there. A reply still waiting to be sent through ` +
			`Microsoft 365 fails and can be retried. The deleted access cannot be undone: connect again with colliedesk_microsoft_connect. ` + emailChannelChoice,
		REST:   []string{"DELETE /api/v1/channels/email/microsoft", "DELETE /api/v1/channels/{id}/email/microsoft"},
		Input:  emailChannelInput,
		Output: doneSchema,
		run:    microsoftDisconnect,
	}
}

func emailSecretRotateSpec() Spec {
	return Spec{
		Name:  "colliedesk_email_secret_rotate",
		Title: "Rotate the inbound email secret",
		Description: `Give an email channel a new inbound secret, for example after the old one leaked; the old one stops working at once, and that cannot be undone. Mail forwarded with the new one arrives on that channel. ` +
			`The new secret is in the answer, shown once: pass it only to the person who updates the mail forwarder, and never write it in a conversation, note or reply. ` + emailChannelChoice,
		REST:   []string{"POST /api/v1/channels/email/secret", "POST /api/v1/channels/{id}/email/secret"},
		Input:  emailChannelInput,
		Output: `{"type":"object","required":["inbound_secret","next"],"properties":{"inbound_secret":{"type":"string"},"next":{"type":"string"}}}`,
		run:    emailSecretRotate,
	}
}

func numberSeriesListSpec() Spec {
	return Spec{
		Name: "colliedesk_number_series_list", Title: "List the conversation number series",
		Description: `List the series the workspace's conversations are numbered in, the default one (#{n}) first: each with its code format, the number the next ` +
			`conversation takes, and where this server took it over (start, start_year). A channel numbers what it opens in the series its settings name (number_series).`,
		REST:   []string{"GET /api/v1/conversation-number-series"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","required":["series","format","next","start"],"properties":{"series":{"type":"string"},"format":{"type":"string"},"next":{"type":"integer"},"start":{"type":"integer"},"start_year":{"type":["integer","null"]},"updated_at":{"type":"string"}}}}}}`,
		run:    numberSeriesList,
	}
}

func responseTargetsGetSpec() Spec {
	return Spec{
		Name: "colliedesk_response_targets_get", Title: "Read the response targets",
		Description: `Read the workspace's response targets: business hours (timezone, weekdays, holidays), first-response and resolution targets by priority in business ` +
			`minutes, how long before a target a conversation is due soon, the channels they apply to and whether reminder emails go out.`,
		REST:   []string{"GET /api/v1/response-targets"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: responseTargetsSchema,
		run:    responseTargetsGet,
	}
}

func mySLAAlertsSetSpec() Spec {
	return Spec{
		Name: "colliedesk_my_sla_alerts_set", Title: "Turn your response target emails on or off",
		Description: `Turn the reminder and overdue emails of response targets on or off for the person this token acts as (colliedesk_response_targets_get shows my_alert_email).`,
		Idempotent:  true,
		REST:        []string{"PATCH /api/v1/agents/me/sla-alerts"},
		Input:       `{"type":"object","additionalProperties":false,"required":["email"],"properties":{"email":{"type":"boolean"}}}`,
		Output:      `{"type":"object","required":["email"],"properties":{"email":{"type":"boolean"}}}`,
		run: func(c *call, arguments map[string]any) (any, error) {
			var out map[string]any
			if err := c.send(http.MethodPatch, "/api/v1/agents/me/sla-alerts", arguments, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

func numberSeriesSetSpec() Spec {
	return Spec{
		Name: "colliedesk_number_series_set", Title: "Add or change a conversation number series",
		Description: `Add a number series or change its format (letters, digits, # - _ / . with {n} once, {n:04} to pad, {yyyy} for the year), next number (it only counts on) or start. ` +
			`List them first with colliedesk_number_series_list.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/conversation-number-series/{series}"},
		Input: `{"type":"object","additionalProperties":false,"required":["series"],"minProperties":2,"properties":{"series":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,19}$"},` +
			`"format":{"type":"string","minLength":3,"maxLength":40},"next":{"type":"integer","minimum":1},"start":{"type":"integer","minimum":1},"start_year":{"type":"integer","minimum":1970,"maximum":9999}}}`,
		Output: `{"type":"object","required":["series","format","next","start"],"properties":{"series":{"type":"string"},"format":{"type":"string"},"next":{"type":"integer"},"start":{"type":"integer"},"start_year":{"type":["integer","null"]},"updated_at":{"type":"string"}}}`,
		run:    numberSeriesSet,
	}
}

func responseTargetsSetSpec() Spec {
	return Spec{
		Name: "colliedesk_response_targets_set", Title: "Change the response targets",
		Description: `Replace the response targets with a whole document (read it first with colliedesk_response_targets_get and change what you need): enabled, timezone, ` +
			`hours (mon to sun, open, from, to), holidays, targets by priority (first_response_minutes, resolution_minutes in business minutes, null for none), ` +
			`warn_before_minutes, channel_ids (null for every channel) and alert_email.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/response-targets"},
		Input: `{"type":"object","additionalProperties":false,"required":["enabled","hours","targets"],"properties":{"enabled":{"type":"boolean"},"timezone":{"type":["string","null"]},` +
			`"hours":{"type":"array","maxItems":7,"items":{"type":"object","required":["day","open"],"properties":{"day":{"type":"string","enum":["mon","tue","wed","thu","fri","sat","sun"]},"open":{"type":"boolean"},"from":{"type":"string"},"to":{"type":"string"}}}},` +
			`"holidays":{"type":"array","maxItems":500,"items":{"type":"object","required":["date","name"],"properties":{"date":{"type":"string"},"name":{"type":"string","maxLength":100}}}},` +
			`"targets":{"type":"object"},"warn_before_minutes":{"type":"integer","minimum":0,"maximum":10080},"channel_ids":{"type":["array","null"],"items":{"type":"string"}},"alert_email":{"type":"boolean"}}}`,
		Output: responseTargetsSchema,
		run:    responseTargetsSet,
	}
}

func workspaceSlugChangeSpec() Spec {
	return Spec{
		Name: "colliedesk_workspace_slug_change", Title: "Change the workspace address",
		Description: `Change the workspace's address (slug), which also changes its hosted support email address and web form link: tell the team and update any forwarding ` +
			`or link that used the old one.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/workspace/slug"},
		Input:      `{"type":"object","additionalProperties":false,"required":["slug"],"properties":{"slug":{"type":"string","minLength":1,"maxLength":63}}}`,
		Output:     `{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"object","properties":{"name":{"type":"string"},"slug":{"type":"string"}}}}}`,
		run:        workspaceSlugChange,
	}
}
