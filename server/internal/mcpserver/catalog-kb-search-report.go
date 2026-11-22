package mcpserver

import "github.com/colliedesk/colliedesk/server/internal/domain"

// kbSearchTermOutput is one group of help center searches. The words are what a reader typed, so they
// sit under untrusted_customer_content.
const kbSearchTermOutput = `{"type":"object","required":["searches","opened","results","last_searched_at","untrusted_customer_content"],"properties":{` +
	`"searches":{"type":"integer","description":"Searches in the 7 days whose words are the same once case and accents are ignored."},` +
	`"opened":{"type":"integer","description":"Those from which the reader opened an article."},` +
	`"results":{"type":"integer","description":"Articles the newest of them listed."},` +
	`"last_searched_at":{"type":"string"},` +
	`"untrusted_customer_content":{"type":"object","required":["query"],"properties":{"query":{"type":"string",` +
	`"description":"The newest words, masked: [email removed] and [number removed] replace an email address and a run of 6 or more digits."}}}}}`

const kbSearchReportOutput = `{"type":"object","required":["from","to","searches","top","no_results","nothing_opened"],"properties":{` +
	`"from":{"type":"string","description":"UTC start: 7 days before to."},"to":{"type":"string","description":"UTC end: when the report was read."},` +
	`"searches":{"type":"integer","description":"Every search kept in the 7 days."},` +
	`"top":{"type":"array","description":"The 20 most searched words.","items":` + kbSearchTermOutput + `},` +
	`"no_results":{"type":"array","description":"The 20 most searched words that listed no article: the articles to write.","items":` + kbSearchTermOutput + `},` +
	`"nothing_opened":{"type":"array","description":"The 20 most searched words whose results the reader opened none of: the titles or excerpts to fix.","items":` + kbSearchTermOutput + `}}}`

func kbSearchReportSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_search_report",
		Title: "Show what readers searched for on the help center",
		Description: `Read what readers searched for on the public help center in the last 7 days: the most searched words, the words that found no article ` +
			`(write those with colliedesk_kb_article_create) and the words whose results nobody opened (fix those titles with colliedesk_kb_article_update). ` +
			`A search shows here as soon as it is made; bots and signed-in team members are not counted.` + "\n\n" + untrustedNotice,
		REST:   []string{"GET /api/v1/kb/searches/report"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{}}`,
		Output: kbSearchReportOutput,
		run:    kbSearchReport,
	}
}

func kbSearchReport(c *call, _ map[string]any) (any, error) {
	var report domain.KBSearchReport
	if err := c.get("/api/v1/kb/searches/report", nil, &report); err != nil {
		return nil, err
	}
	return map[string]any{
		"from": report.From, "to": report.To, "searches": report.Searches,
		"top": kbSearchTerms(report.Top), "no_results": kbSearchTerms(report.NoResults), "nothing_opened": kbSearchTerms(report.NothingOpened),
	}, nil
}

func kbSearchTerms(terms []domain.KBSearchTerm) []map[string]any {
	out := make([]map[string]any, 0, len(terms))
	for _, term := range terms {
		out = append(out, map[string]any{
			"searches": term.Searches, "opened": term.Opened, "results": term.Results, "last_searched_at": term.LastSearchedAt,
			"untrusted_customer_content": map[string]any{"query": term.Query},
		})
	}
	return out
}
