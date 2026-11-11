package domain

import (
	"regexp"
	"time"
)

// attributionValue is what colliedesk.com accepts in its own links: plain tokens, never free text, so
// a tracking parameter cannot carry an email address or markup into the database or the funnel.
var attributionValue = regexp.MustCompile(`^[A-Za-z0-9._-]{1,300}$`)

// referrerHost is a lowercase DNS name with at least one dot: the host of the page that linked to the sign-in
// page, never its path or query. A URL, a single label such as localhost or an uppercase spelling is dropped.
var referrerHost = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// clickIDMaxAge bounds the click time a browser reports. The sign-in page remembers a landing for a day;
// anything older or in the future is not a time this server can trust, so the sign-up time stands in.
const clickIDMaxAge = 7 * 24 * time.Hour

// Attribution is the post or ad a cloud sign-up came from: the utm_* keys of the link and the click IDs
// TikTok (ttclid) and Meta (fbclid) add to ad links, and the site that linked to the sign-in page. An empty
// field was not given.
type Attribution struct {
	UTMSource, UTMMedium, UTMCampaign, UTMContent, UTMTerm string
	TTCLID, FBCLID                                         string
	// ReferrerHost is the site whose link first brought the browser to the sign-in page (claude.ai, a blog),
	// which is all an AI assistant's link without utm_* keys says. Empty when the browser named none.
	ReferrerHost string
	// ClickIDSeenAt is when the browser first saw a click ID; Meta reads it as the click time (fbc).
	// Zero when there is no click ID.
	ClickIDSeenAt time.Time
}

// ParseAttribution keeps the listed keys whose values are plain tokens and drops everything else.
// click_id_seen_at is an RFC 3339 time, kept only with a click ID; without a believable one the click
// counts as seen now.
func ParseAttribution(values map[string]string, now time.Time) Attribution {
	keep := func(key string) string {
		if value := values[key]; attributionValue.MatchString(value) {
			return value
		}
		return ""
	}
	parsed := Attribution{
		UTMSource: keep("utm_source"), UTMMedium: keep("utm_medium"), UTMCampaign: keep("utm_campaign"),
		UTMContent: keep("utm_content"), UTMTerm: keep("utm_term"), TTCLID: keep("ttclid"), FBCLID: keep("fbclid"),
	}
	if host := values["referrer"]; len(host) <= 253 && referrerHost.MatchString(host) {
		parsed.ReferrerHost = host
	}
	if parsed.TTCLID == "" && parsed.FBCLID == "" {
		return parsed
	}
	parsed.ClickIDSeenAt = now
	if seen, err := time.Parse(time.RFC3339, values["click_id_seen_at"]); err == nil && !seen.After(now) && now.Sub(seen) <= clickIDMaxAge {
		parsed.ClickIDSeenAt = seen.UTC()
	}
	return parsed
}

// Properties returns the given values by key, for the trial_started and subscription_activated
// funnel events; nil when the sign-up came with none.
func (a Attribution) Properties() map[string]any {
	var properties map[string]any
	for key, value := range a.fields() {
		if value == "" {
			continue
		}
		if properties == nil {
			properties = map[string]any{}
		}
		properties[key] = value
	}
	return properties
}

func (a Attribution) fields() map[string]string {
	return map[string]string{
		"utm_source": a.UTMSource, "utm_medium": a.UTMMedium, "utm_campaign": a.UTMCampaign,
		"utm_content": a.UTMContent, "utm_term": a.UTMTerm, "ttclid": a.TTCLID, "fbclid": a.FBCLID,
		"referrer_host": a.ReferrerHost,
	}
}
