package telemetry

import (
	"net/netip"
	"net/url"
	"strings"
)

// endpoint is the PostHog host that receives the report.
const endpoint = "https://us.i.posthog.com"

// projectKey is the public, write-only PostHog project key of Colliedesk usage reports. It can
// only add events; it cannot read anything. Empty means this build sends nothing.
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
// and describes this install from its public origin.
func ConfigFromEnv(getenv func(string) string, origin string, cloud bool, version string) Config {
	config := Config{Endpoint: endpoint, ProjectKey: projectKey, Version: version, Realm: "self-hosted", Domain: publicHost(origin)}
	if cloud {
		config.Realm = "cloud"
	}
	switch {
	case truthy(getenv("DISABLE_TELEMETRY")):
		config.OffReason = "DISABLE_TELEMETRY is set"
	case truthy(getenv("DO_NOT_TRACK")):
		config.OffReason = "DO_NOT_TRACK is set"
	}
	return config
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// privateSuffixes are names that only resolve inside one network.
var privateSuffixes = []string{".localhost", ".local", ".lan", ".internal", ".home.arpa", ".localdomain"}

// publicHost returns the lower-case host of origin when it is a public DNS name. Addresses,
// localhost, single-label and private names say nothing useful and are not sent.
func publicHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || host == "localhost" || !strings.Contains(host, ".") {
		return ""
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return ""
	}
	for _, suffix := range privateSuffixes {
		if strings.HasSuffix(host, suffix) {
			return ""
		}
	}
	return host
}
