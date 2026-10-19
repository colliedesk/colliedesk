package httpapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// posthogPersonURL opens a person in the Colliedesk PostHog project; the person id is the agent id.
const posthogPersonURL = "https://us.posthog.com/project/666761/person/"

// reportBody is the text of the support desk's message: the description, then every fact the team needs to
// check the report. Facts come from the signed-in session and the server, never from the reporter's data.
func (a *API) reportBody(who auth.Principal, description string, diagnostics diagnosticsInput, requestID string, now time.Time) string {
	var out strings.Builder
	out.WriteString(description)
	out.WriteString("\n\n---\n")
	name := who.Agent.DisplayName
	if name == "" {
		name = who.Agent.Email
	}
	signedIn := "person"
	if who.Scope != "" {
		signedIn = "agent token (" + who.Scope + ")"
	}
	plan := who.Access.Plan
	if plan == "" {
		plan = "none (no plan limits)"
	}
	fmt.Fprintf(&out, "Reporter: %s <%s>, %s, agent id %s, signed in as %s\n", oneLine(name, 200), who.Agent.Email, who.Agent.Role, who.Agent.ID, signedIn)
	fmt.Fprintf(&out, "Workspace: %s (%s), id %s, plan %s\n", oneLine(who.Workspace.Name, 200), who.Workspace.Slug, who.Workspace.ID, plan)
	fmt.Fprintf(&out, "Server: version %s, realm %s\n", a.version(), a.realm())
	fmt.Fprintf(&out, "Report request id: %s\n", requestID)
	fmt.Fprintf(&out, "Reported at: %s\n", now.UTC().Format(time.RFC3339))
	out.WriteString("\nBrowser\n")
	fmt.Fprintf(&out, "- App version: %s\n- User agent: %s\n- Page: %s\n", noneIfEmpty(diagnostics.AppVersion), noneIfEmpty(diagnostics.UserAgent), noneIfEmpty(diagnostics.PagePath))
	if v := diagnostics.Viewport; v != nil {
		fmt.Fprintf(&out, "- Viewport: %dx%d\n", v.Width, v.Height)
	} else {
		out.WriteString("- Viewport: none\n")
	}
	fmt.Fprintf(&out, "\nClient errors (%d)\n", len(diagnostics.ClientErrors))
	for _, item := range diagnostics.ClientErrors {
		fmt.Fprintf(&out, "- %s %s: %s\n", item.At, noneIfEmpty(item.Source), item.Message)
	}
	fmt.Fprintf(&out, "\nFailed API calls (%d)\n", len(diagnostics.FailedAPICalls))
	for _, item := range diagnostics.FailedAPICalls {
		fmt.Fprintf(&out, "- %s %s %s -> %d, request id %s\n", item.At, item.Method, item.Route, item.Status, noneIfEmpty(item.RequestID))
	}
	fmt.Fprintf(&out, "\nRecent product events: %s\n", noneIfEmpty(strings.Join(diagnostics.ProductEvents, ", ")))
	if a.config.ProductAnalytics != nil {
		fmt.Fprintf(&out, "\nPostHog: %s%s\n", posthogPersonURL, who.Agent.ID)
	}
	return out.String()
}

func noneIfEmpty(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func (a *API) version() string {
	if a.config.Version == "" {
		return "dev"
	}
	return a.config.Version
}

// realm is "cloud" on Colliedesk Cloud (cloud sign-in with billing) and "self-hosted" everywhere else; the
// value the web app's product events carry when they are on.
func (a *API) realm() string {
	if analytics := a.config.ProductAnalytics; analytics != nil && analytics.Realm != "" {
		return analytics.Realm
	}
	if a.config.CloudAuth.Enabled() && a.config.Billing.Enabled() {
		return "cloud"
	}
	return "self-hosted"
}
