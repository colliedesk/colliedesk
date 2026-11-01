package store

// SetupLinks gives the guide, MCP and import emails the same action links.
// helpCenterURL comes from the server's help center address configuration.
func SetupLinks(progress *SetupProgress, helpCenterURL string) {
	targets := map[string]string{"email": "/settings/channels/type/email", "import": "/settings/import-freshdesk", "invite": "/settings", "first_reply": "/",
		"custom_domain": "/settings#help-center-domain-heading", "plan": "/settings/billing", "cname": "/settings#help-center-domain-heading", "search_console": "/settings#help-center-search-console-heading"}
	if progress.HelpCenter != nil {
		progress.HelpCenter.URL = helpCenterURL
		if progress.HelpCenter.Action == "enable" {
			progress.HelpCenter.URL = "/settings#help-center-heading"
		}
	}
	for i := range progress.Steps {
		step := &progress.Steps[i]
		step.URL = targets[step.Key]
		if step.Detail != nil && *step.Detail == "enable" {
			step.URL = "/settings#help-center-heading"
		}
		done := map[string]string{"email": "working", "import": "done", "invite": "joined", "first_reply": "sent"}[step.Key]
		if done == "" {
			done = "done"
		}
		if progress.NextURL == nil && step.State != done && step.State != "skipped" {
			next := step.URL
			progress.NextURL = &next
		}
	}
}
