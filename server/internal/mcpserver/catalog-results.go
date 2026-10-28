package mcpserver

func resultsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_get",
		Title: "Show the results page",
		Description: `Show the team's numbers in Colliedesk next to the baseline of the history it imported (Freshdesk), for the last 30 and 7 days: ` +
			`first response time (median, p90), time to resolve (resolutions that stayed resolved 7 days), reopened within 7 days, conversations per week, ` +
			`handle time from the import, AI drafts sent nearly unchanged, edited or discarded, and hours saved (an upper bound). ` +
			`Nothing is computed when you call it: computed_at says when the numbers were worked out. A median below 5 values is null. ` +
			`To read the conversations behind a number, call colliedesk_inbox_search with result "<snapshot id>:<window>.<metric>" (for example the current id and "30d.first_response"). ` +
			`How each number is counted: colliedesk_docs_search "results".` +
			"\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/results"},
		Input: noArguments,
		Output: `{"type":"object","required":["results"],"properties":{"results":{"type":"object","required":["current","baseline","baseline_state","hours_saved"],"properties":{` +
			`"current":{"type":["object","null"]},"baseline":{"type":["object","null"]},` +
			`"baseline_state":{"type":"string","enum":["none","computing","ready"]},"hours_saved":{"type":"object"},"error":{"type":["object","null"]}}}}}`,
		run: resultsGet,
	}
}

const shareLinkOutput = `{"type":"object","required":["link"],"properties":{"link":{"type":["object","null"],"properties":{"created_at":{"type":"string"},` +
	`"created_by_name":{"type":["string","null"]},"last_viewed_at":{"type":["string","null"]}}}}}`

func resultsShareLinkGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_share_link_get",
		Title: "Show the results share link",
		Description: `Show whether the results page has an active share link (anyone with it sees the totals without signing in), who made it, when, and when it was last opened. ` +
			`Never the link itself: it is shown once, to the person who makes it.` + "\n\nTier: admin.",
		Tier: "admin", ReadOnly: true,
		REST:   []string{"GET /api/v1/results/share-link"},
		Input:  noArguments,
		Output: shareLinkOutput,
		run:    resultsShareLinkGet,
	}
}

func resultsShareLinkCreateSpec() Spec {
	return approvalSpec("colliedesk_results_share_link_create", "Ask for a share link of the results page",
		`Ask a person to create a link that shows the results page's totals to anyone who has it, without signing in (no names, customers or messages). `+
			`The old link stops working. The person who approves gets the link; it is never sent to you.`,
		[]string{"POST /api/v1/results/share-link"}, noArguments, resultsShareLinkCreate)
}

func resultsShareLinkRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_share_link_revoke",
		Title: "Turn off the results share link",
		Description: `Turn off the results page's share link: anyone who opens it then reads that it was turned off. Safe to repeat.` +
			"\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"DELETE /api/v1/results/share-link"},
		Input:  noArguments,
		Output: shareLinkOutput,
		run:    resultsShareLinkRevoke,
	}
}

func resultsEmailSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_email_set",
		Title: "Turn your weekly results email on or off",
		Description: `Turn the weekly results email on or off for the team member this token acts as. It goes to admins only, on Mondays at 8:00 in the workspace's timezone, ` +
			`when the server can send email. Safe to repeat. With a read token Colliedesk files it as an agent request.` + "\n\nTier: draft.",
		Tier: "draft", Idempotent: true,
		REST:   []string{"PATCH /api/v1/agents/me/results-email"},
		Input:  `{"type":"object","additionalProperties":false,"required":["weekly"],"properties":{"weekly":{"type":"boolean"}}}`,
		Output: `{"type":"object","required":["weekly"],"properties":{"weekly":{"type":"boolean"}}}`,
		run:    resultsEmailSet,
	}
}
