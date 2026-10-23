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
