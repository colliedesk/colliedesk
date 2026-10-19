package telemetry

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
)

// visitorIDPattern is the random UUID PostHog gives a website visitor. Anything else (the
// cookieless placeholder, an identified email, a forged value) is not passed on.
var visitorIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// WebsiteVisitorID returns the PostHog id colliedesk.com gave the browser that sent request, or
// an empty string. The website sets that cookie on .colliedesk.com only after the visitor accepts
// cookies in its banner, so the hosted app sees it and a trial can be linked to the visit that led
// to it. A browser that declined, or never visited the website, has no cookie.
func WebsiteVisitorID(request *http.Request) string {
	cookie, err := request.Cookie("ph_" + projectKey + "_posthog")
	if err != nil {
		return ""
	}
	raw, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return ""
	}
	var state struct {
		DistinctID string `json:"distinct_id"`
	}
	if json.Unmarshal([]byte(raw), &state) != nil || !visitorIDPattern.MatchString(state.DistinctID) {
		return ""
	}
	return state.DistinctID
}
