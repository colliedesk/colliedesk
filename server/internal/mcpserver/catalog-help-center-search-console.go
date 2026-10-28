package mcpserver

import "net/http"

const (
	searchConsoleTokenSchema = `{"type":["string","null"],"maxLength":100}`
	searchConsoleInput       = `{"type":"object","additionalProperties":false,"properties":{` +
		`"google_site_verification":{"type":["string","null"],"maxLength":100,"description":"The content value of Google Search Console's google-site-verification meta tag (16 to 100 letters, digits, - or _), not the whole tag. Left out: unchanged. Null or empty: removed."},` +
		`"bing_site_verification":{"type":["string","null"],"maxLength":100,"description":"The content value of Bing Webmaster Tools' msvalidate.01 meta tag. Left out: unchanged. Null or empty: removed."}}}`
	searchConsoleSchema = `{"type":"object","required":["google_site_verification","bing_site_verification","content_version"],"properties":{` +
		`"google_site_verification":` + searchConsoleTokenSchema + `,"bing_site_verification":` + searchConsoleTokenSchema + `,` +
		`"content_version":{"type":"integer"},"updated_at":{"type":["string","null"]}}}`
)

func helpCenterSearchConsoleGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_search_console_get",
		Title: "Read the help center's Search Console tokens",
		Description: `Return the Google and Bing verification tokens of the public help center (null when not set). ` +
			`Each is served as a meta tag on this workspace's help center pages only.` + "\n\nTier: read.",
		Tier: "read", ReadOnly: true,
		REST:   []string{"GET /api/v1/help-center/search-console"},
		Input:  noArguments,
		Output: `{"type":"object","required":["search_console"],"properties":{"search_console":` + searchConsoleSchema + `}}`,
		run:    helpCenterSearchConsoleGet,
	}
}

func helpCenterSearchConsoleUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_search_console_update",
		Title: "Set the help center's Search Console tokens",
		Description: `Set or remove the Google (google-site-verification) and Bing (msvalidate.01) verification tokens so the team can verify its help center host in Search Console and Webmaster Tools. ` +
			`Send only the content value of the meta tag, not the whole tag; anything else is refused and nothing is saved. A field left out stays; null or "" removes it. ` +
			`The tags change every public page: a token needs help_center:update and kb_articles:publish; without either it files a request for a person to approve. ` +
			`It is safe to repeat. ` + belowAdmin + "\n\nTier: admin.",
		Tier: "admin", Idempotent: true,
		REST:   []string{"PUT /api/v1/help-center/search-console"},
		Input:  searchConsoleInput,
		Output: `{"type":"object","required":["search_console"],"properties":{"search_console":` + searchConsoleSchema + `}}`,
		run:    helpCenterSearchConsoleUpdate,
	}
}

func helpCenterSearchConsoleGet(c *call, _ map[string]any) (any, error) {
	var tokens map[string]any
	if err := c.get("/api/v1/help-center/search-console", nil, &tokens); err != nil {
		return nil, err
	}
	return map[string]any{"search_console": tokens}, nil
}

func helpCenterSearchConsoleUpdate(c *call, arguments map[string]any) (any, error) {
	var tokens map[string]any
	if err := c.send(http.MethodPut, "/api/v1/help-center/search-console", arguments, &tokens); err != nil {
		return nil, err
	}
	return map[string]any{"search_console": tokens}, nil
}
