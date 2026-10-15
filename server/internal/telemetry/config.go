package telemetry

import (
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// endpoint is the PostHog host that receives the report.
const endpoint = "https://us.i.posthog.com"

// projectKey is the public PostHog project key of Colliedesk usage reports. It can add events
// (anyone can, so the reports are treated as untrusted) but cannot read them. Empty means this
// build sends nothing.
const projectKey = ""

// Config says where the report goes and what describes this install.
type Config struct {
	Endpoint   string
	ProjectKey string
	Version    string
	// Realm is "cloud" on the hosted service and "self-hosted" everywhere else.
	Realm string
	// Domain is the public host name of APP_ORIGIN, or empty when it is local or private.
	Domain string
	// OffReason names the setting that turned the report off; empty means on.
	OffReason string
}

// Enabled reports whether this server sends the report.
func (c Config) Enabled() bool { return c.OffReason == "" && c.ProjectKey != "" }

// ConfigFromEnv reads the opt-out settings (DISABLE_TELEMETRY, or the cross-tool DO_NOT_TRACK)
// and describes this install from its public origin. Development builds never report.
func ConfigFromEnv(getenv func(string) string, origin string, cloud bool, version string) Config {
	config := Config{Endpoint: endpoint, ProjectKey: projectKey, Version: version, Realm: "self-hosted", Domain: publicHost(origin)}
	if cloud {
		config.Realm = "cloud"
	}
	switch {
	case optedOut(getenv("DISABLE_TELEMETRY")):
		config.OffReason = "DISABLE_TELEMETRY is set"
	case optedOut(getenv("DO_NOT_TRACK")):
		config.OffReason = "DO_NOT_TRACK is set"
	case version == "dev":
		config.OffReason = "this is a development build"
	}
	return config
}

// optedOut fails closed: any value except empty, 0, false, no or off turns the report off.
func optedOut(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// publicHost returns the host of origin in lower-case ASCII when it is a name under an ICANN
// public suffix (help.example.com). Addresses, localhost, private or special names (.lan, .corp,
// .onion, a tailnet) and names that embed an IPv4 address (10.0.0.5.nip.io) are not sent.
func publicHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	host, err := idna.Lookup.ToASCII(strings.TrimRight(parsed.Hostname(), "."))
	if err != nil || host == "" || strings.HasSuffix(host, ".onion") || embedsIPv4(host) {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	suffix, icann := publicsuffix.PublicSuffix(host)
	if !icann || host == suffix {
		return ""
	}
	return host
}

// embedsIPv4 reports whether four consecutive labels are all digits, as in wildcard DNS names.
func embedsIPv4(host string) bool {
	run := 0
	for label := range strings.SplitSeq(host, ".") {
		if label != "" && strings.Trim(label, "0123456789") == "" {
			run++
			if run == 4 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}
