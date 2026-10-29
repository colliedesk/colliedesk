package mcpserver

import "net/url"

func helpCenterFreshdeskRedirectGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_freshdesk_redirect_get",
		Title: "Read how the old Freshdesk help center addresses move",
		Description: `For a team that moved from <name>.freshdesk.com, say what to do about the old help center addresses, and, on the Freshdesk Pro and Enterprise plans, return the HTML snippet to paste into the Freshdesk portal's head. ` +
			`The snippet is a JavaScript redirect (not a 301) from each old article, folder and category address to its new page, built from the help center's address and the pages the import made public; it works only while the Freshdesk account does, so keep both for keep_weeks after the switch. ` +
			`Send plan (growth, pro or enterprise), the Freshdesk plan of the account: state ask_plan means send it; growth cannot paste code into the portal, so old *.freshdesk.com links cannot be redirected and no snippet is returned. ` +
			`State own_domain means the old help center had its own domain: redirect it at DNS. no_import and help_center_off say what is missing.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:  []string{"GET /api/v1/help-center/freshdesk-redirect"},
		Input: `{"type":"object","additionalProperties":false,"properties":{"plan":{"type":"string","enum":["growth","pro","enterprise"],"description":"The Freshdesk plan of the account."}}}`,
		Output: `{"type":"object","required":["redirect"],"properties":{"redirect":{"type":"object","required":["state","freshdesk_domain","address","keep_weeks","snippet","mapped"],"properties":{` +
			`"state":{"type":"string","enum":["no_import","own_domain","help_center_off","ask_plan","growth","ready"]},"freshdesk_domain":{"type":["string","null"]},"address":{"type":"string"},` +
			`"keep_weeks":{"type":"integer"},"snippet":{"type":["string","null"]},"mapped":{"type":["object","null"]}}}}}`,
		run: helpCenterFreshdeskRedirectGet,
	}
}

func helpCenterFreshdeskRedirectGet(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	if plan, ok := arguments["plan"].(string); ok && plan != "" {
		query.Set("plan", plan)
	}
	var redirect map[string]any
	if err := c.get("/api/v1/help-center/freshdesk-redirect", query, &redirect); err != nil {
		return nil, err
	}
	return map[string]any{"redirect": redirect}, nil
}
