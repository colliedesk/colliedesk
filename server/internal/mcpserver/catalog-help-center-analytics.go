package mcpserver

import "net/http"

const (
	analyticsText     = `{"type":["string","null"]}`
	analyticsTime     = `{"type":["string","null"],"format":"date-time"}`
	analyticsSettings = `{"type":"object","required":["provider","enabled","ga4_measurement_id","ga4_api_secret_set","plausible_domain","plausible_base_url",` +
		`"last_sent_at","last_error","last_error_at","updated_at","updated_by_name"],"properties":{` +
		`"provider":{"type":["string","null"],"enum":["ga4","plausible",null],"description":"Where page views go: ga4 (Google Analytics 4) or plausible; null before the first save."},` +
		`"enabled":{"type":"boolean","description":"Whether page views are sent now."},` +
		`"ga4_measurement_id":` + analyticsText + `,` +
		`"ga4_api_secret_set":{"type":"boolean","description":"Whether a Measurement Protocol API secret is stored; the secret itself is never answered."},` +
		`"plausible_domain":` + analyticsText + `,"plausible_base_url":{"type":"string"},` +
		`"last_sent_at":` + analyticsTime + `,"last_error":` + analyticsText + `,"last_error_at":` + analyticsTime + `,` +
		`"updated_at":` + analyticsTime + `,"updated_by_name":` + analyticsText + `}}`
	analyticsInput = `{"type":"object","additionalProperties":false,"required":["provider","enabled"],"properties":{` + helpCenterIDProperty + `,` +
		`"provider":{"type":"string","enum":["ga4","plausible"],"description":"ga4 (Google Analytics 4) or plausible."},` +
		`"enabled":{"type":"boolean","description":"true sends every reader's page view from now on; false keeps the settings and sends nothing."},` +
		`"ga4_measurement_id":{"type":["string","null"],"maxLength":20,"description":"The measurement ID of the GA4 web stream, for example G-ABC123XYZ9. Needed when provider is ga4 and enabled is true."},` +
		`"ga4_api_secret":{"type":["string","null"],"maxLength":100,"description":"The secret value of a Measurement Protocol API secret (GA4 Admin > Data streams > the web stream). Left out or null: the stored one stays. Empty: removed. Never answered."},` +
		`"plausible_domain":{"type":["string","null"],"maxLength":253,"description":"The site domain exactly as it is in Plausible, for example help.example.com. Needed when provider is plausible and enabled is true."},` +
		`"plausible_base_url":{"type":["string","null"],"maxLength":200,"description":"Where Plausible runs: https://plausible.io (the default when left out) or a self-hosted Plausible on a public https address."}}}`
	analyticsOutput     = `{"type":"object","required":["analytics"],"properties":{"analytics":` + analyticsSettings + `}}`
	analyticsTestOutput = `{"type":"object","required":["test"],"properties":{"test":{"type":"object","required":["ok","message"],"properties":{` +
		`"ok":{"type":"boolean","description":"true when the service accepted the test page view."},` +
		`"message":{"type":"string","description":"What the service answered and where to see the event, or what to fix."}}}}}`
)

func helpCenterAnalyticsGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_analytics_get",
		Title: "Read where the help center sends its page views",
		Description: `Return the help center's analytics: the workspace's own Google Analytics 4 or Plausible, whether it is on, ` +
			`when a page view was last accepted and the last error. The server sends each reader's page view itself; no script is put on the pages. ` +
			`The GA4 API secret is never returned, only whether one is stored.`,
		REST:   []string{"GET /api/v1/help-center/analytics"},
		Input:  helpCenterOnlyInput,
		Output: analyticsOutput,
		run:    helpCenterAnalyticsGet,
	}
}

func helpCenterAnalyticsSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_analytics_set",
		Title: "Send the help center's page views to GA4 or Plausible",
		Description: `Save where the help center's page views go: provider ga4 with a measurement ID and a Measurement Protocol API secret, ` +
			`or plausible with the site domain (and the address of a self-hosted Plausible). Every field replaces the stored one except ga4_api_secret, which stays when left out. ` +
			`GA4 gets the page address, title, referrer, language and search words, with a client id that changes every day instead of the reader's address; ` +
			`Plausible gets the page address and referrer with the reader's browser and address, which it turns into its own daily visitor id. ` +
			`An invalid field is refused by name and nothing is saved. Call colliedesk_help_center_analytics_test next to check the service accepts the events. It is safe to repeat.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/help-center/analytics"},
		Input:      analyticsInput,
		Output:     analyticsOutput,
		run:        helpCenterAnalyticsSet,
	}
}

func helpCenterAnalyticsTestSpec() Spec {
	return Spec{
		Name:  "colliedesk_help_center_analytics_test",
		Title: "Send a test page view to GA4 or Plausible",
		Description: `Send one test page view of /colliedesk-analytics-test to the saved GA4 or Plausible now, on or off, and say what the service answered. ` +
			`GA4 names a wrong measurement ID; it never checks the API secret, so look for the event in its Realtime report. ` +
			`Before the first save it is refused and says to call colliedesk_help_center_analytics_set.`,
		REST:   []string{"POST /api/v1/help-center/analytics/test"},
		Input:  helpCenterOnlyInput,
		Output: analyticsTestOutput,
		run:    helpCenterAnalyticsTest,
	}
}

func helpCenterAnalyticsGet(c *call, arguments map[string]any) (any, error) {
	path, _ := c.atHelpCenter("/api/v1/help-center/analytics", arguments)
	var settings map[string]any
	if err := c.get(path, nil, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"analytics": settings}, nil
}

func helpCenterAnalyticsSet(c *call, arguments map[string]any) (any, error) {
	path, body := c.atHelpCenter("/api/v1/help-center/analytics", arguments)
	var settings map[string]any
	if err := c.send(http.MethodPut, path, body, &settings); err != nil {
		return nil, err
	}
	return map[string]any{"analytics": settings}, nil
}

func helpCenterAnalyticsTest(c *call, arguments map[string]any) (any, error) {
	path, _ := c.atHelpCenter("/api/v1/help-center/analytics/test", arguments)
	var result map[string]any
	if err := c.send(http.MethodPost, path, map[string]any{}, &result); err != nil {
		return nil, err
	}
	return map[string]any{"test": result}, nil
}
