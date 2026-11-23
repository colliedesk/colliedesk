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
			`help_center is read live: help center article views, requests opened (colliedesk_inbox_search with source native, created_after from, created_before to) and the self-service score, views per request (null without a request), per week and per day, ` +
			`and the most viewed articles with their helpful votes and the sent replies that used them (colliedesk_kb_article_stats for one article per day). ` +
			`help_center_id (colliedesk_help_centers_list) narrows help_center to one help center: the articles it shows and their views, and the requests that came in through it; the other numbers stay the workspace's. ` +
			`How each number is counted: colliedesk_docs_search "results".`,
		REST: []string{"GET /api/v1/results"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"help_center_id":{"type":"string",` + uuid + `,` +
			`"description":"One of colliedesk_help_centers_list; left out, help_center reads the whole workspace."}}}`,
		Output: `{"type":"object","required":["results"],"properties":{"results":{"type":"object","required":["current","baseline","baseline_state","hours_saved","help_center"],"properties":{` +
			`"current":{"type":["object","null"]},"baseline":{"type":["object","null"]},` +
			`"baseline_state":{"type":"string","enum":["none","computing","ready"]},"hours_saved":{"type":"object"},"error":{"type":["object","null"]},"help_center":` + resultsHelpCenterOutput + `}}}}`,
		run: resultsGet,
	}
}

// resultsHelpCenterOutput is the help center section per window (30d, 7d): every day and week is a
// calendar day or week in timezone, and each range is [from, to).
const (
	resultsHelpCenterCounts = `"from":{"type":"string"},"to":{"type":"string"},"views":{"type":"integer"},"requests":{"type":"integer"},"score":{"type":["number","null"]}`
	resultsHelpCenterWindow = `{"type":"object","required":["from","to","views","requests","score","weeks","days","articles","articles_total"],"properties":{` + resultsHelpCenterCounts + `,` +
		`"weeks":{"type":"array","items":{"type":"object","required":["from","to","first_day","last_day","views","requests","score"],"properties":{` + resultsHelpCenterCounts + `,"first_day":{"type":"string"},"last_day":{"type":"string"}}}},` +
		`"days":{"type":"array","items":{"type":"object","required":["day","views","requests"],"properties":{"day":{"type":"string"},"views":{"type":"integer"},"requests":{"type":"integer"}}}},` +
		`"articles":{"type":"array","description":"At most 50, most viewed first; helpful and votes are what the article's page shows now.","items":{"type":"object","required":["id","number","title","views","helpful","votes","helpful_percent","uses"],"properties":{` +
		`"id":{"type":"string"},"number":{"type":"integer"},"title":{"type":"string"},"views":{"type":"integer"},"helpful":{"type":"integer"},"votes":{"type":"integer"},"helpful_percent":{"type":["integer","null"]},"uses":{"type":"integer"}}}},` +
		`"articles_total":{"type":"integer"}}}`
	resultsHelpCenterOutput = `{"type":"object","required":["timezone","windows"],"properties":{"timezone":{"type":"string"},` +
		`"windows":{"type":"object","required":["30d","7d"],"properties":{"30d":` + resultsHelpCenterWindow + `,"7d":` + resultsHelpCenterWindow + `}}}}`
)

const shareLinkOutput = `{"type":"object","required":["link"],"properties":{"link":{"type":["object","null"],"properties":{"created_at":{"type":"string"},` +
	`"created_by_name":{"type":["string","null"]},"last_viewed_at":{"type":["string","null"]}}}}}`

func resultsShareLinkGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_share_link_get",
		Title: "Show the results share link",
		Description: `Show whether the results page has an active share link (anyone with it sees the totals without signing in), who made it, when, and when it was last opened. ` +
			`Never the link itself: it is shown once, to the person who makes it.`,
		REST:   []string{"GET /api/v1/results/share-link"},
		Input:  noArguments,
		Output: shareLinkOutput,
		run:    resultsShareLinkGet,
	}
}

func resultsShareLinkCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_share_link_create",
		Title: "Create a share link of the results page",
		Description: `Create a link that shows the results page's totals to anyone who has it, without signing in (no names, customers or messages). ` +
			`The old link stops working. The answer holds the link, shown this once; give it to the person you work for. Revoke it with colliedesk_results_share_link_revoke.`,
		REST:   []string{"POST /api/v1/results/share-link"},
		Input:  noArguments,
		Output: `{"type":"object","required":["link","url","next"],"properties":{"link":{"type":"object"},"url":{"type":"string"},"next":{"type":"string"}}}`,
		run:    resultsShareLinkCreate,
	}
}

func resultsShareLinkRevokeSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_share_link_revoke",
		Title: "Turn off the results share link",
		Description: `Turn off the results page's share link: anyone who opens it then reads that it was turned off. It cannot be undone: ` +
			`colliedesk_results_share_link_create makes a new link at a new address. Safe to repeat.`,
		Idempotent: true,
		REST:       []string{"DELETE /api/v1/results/share-link"},
		Input:      noArguments,
		Output:     shareLinkOutput,
		run:        resultsShareLinkRevoke,
	}
}

func resultsEmailSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_results_email_set",
		Title: "Turn your weekly results email on or off",
		Description: `Turn the weekly results email on or off for the team member this token acts as. It goes only to people who hold results:share, on Mondays at 8:00 in the workspace's timezone, ` +
			`when the server can send email. Safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/agents/me/results-email"},
		Input:      `{"type":"object","additionalProperties":false,"required":["weekly"],"properties":{"weekly":{"type":"boolean"}}}`,
		Output:     `{"type":"object","required":["weekly"],"properties":{"weekly":{"type":"boolean"}}}`,
		run:        resultsEmailSet,
	}
}
