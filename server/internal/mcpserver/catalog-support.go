package mcpserver

const problemDiagnosticsSchema = `{"type":"object","additionalProperties":false,"description":"Optional facts that help the team check the report: ids, routes, status codes and error messages, never what a customer wrote.",` +
	`"properties":{"app_version":{"type":"string","maxLength":40},"user_agent":{"type":"string","maxLength":300},` +
	`"viewport":{"type":"object","required":["width","height"],"properties":{"width":{"type":"integer","minimum":0,"maximum":20000},"height":{"type":"integer","minimum":0,"maximum":20000}}},` +
	`"page_path":{"type":"string","maxLength":300,"description":"Address path; a query string is dropped."},` +
	`"client_errors":{"type":"array","maxItems":20,"items":{"type":"object","required":["message","source","at"],"properties":{"message":{"type":"string","maxLength":2000},"source":{"type":"string","maxLength":100},"at":{"type":"string","description":"RFC 3339 time"}}}},` +
	`"failed_api_calls":{"type":"array","maxItems":20,"items":{"type":"object","required":["method","route","status","at"],"properties":{"method":{"type":"string","maxLength":10},"route":{"type":"string","maxLength":300},` +
	`"status":{"type":"integer","minimum":100,"maximum":599},"request_id":{"type":"string","maxLength":64,"description":"The X-Request-Id of the failed response."},"at":{"type":"string","description":"RFC 3339 time"}}}},` +
	`"product_events":{"type":"array","maxItems":30,"items":{"type":"string","maxLength":60}}}}`

func problemReportSpec() Spec {
	return Spec{
		Name:  "colliedesk_problem_report",
		Title: "Report a problem to the Colliedesk team",
		Description: `Send a problem report to the team that runs this Colliedesk server, the same as "Report a problem" in the web app. ` +
			`Describe what went wrong and what you expected; add the request ids of failed calls (the X-Request-Id of a response, or error.request_id) in diagnostics so the team can find them in the server log. ` +
			`The report carries your description, the diagnostics you give, and facts the server knows (who sent it, the workspace name and plan, the version); no conversation, contact or message text is added. ` +
			`One agent may send 5 reports an hour. A server that has no support desk (most self-hosted installs) refuses with a pointer to open a GitHub issue. Sending it twice files two reports.` +
			"\n\nTier: read.",
		Tier: "read",
		REST: []string{"POST /api/v1/support/problem-reports"},
		Input: `{"type":"object","additionalProperties":false,"required":["description"],"properties":{"description":{"type":"string","minLength":1,"maxLength":5000,` +
			`"description":"What went wrong and what you expected."},"diagnostics":` + problemDiagnosticsSchema + `}}`,
		Output: `{"type":"object","required":["reference"],"properties":{"reference":{"type":"string","description":"Quote this when you write to support."}}}`,
		run:    problemReport,
	}
}
