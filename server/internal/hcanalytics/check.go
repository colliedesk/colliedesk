package hcanalytics

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

var (
	measurementID = regexp.MustCompile(`^G-[A-Z0-9]{4,16}$`)
	apiSecret     = regexp.MustCompile(`^[A-Za-z0-9_-]{8,100}$`)
	siteDomain    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// Settings is a workspace's analytics as the API answers it. The API secret is never in it, only
// whether one is stored.
type Settings struct {
	Provider      *string    `json:"provider"`
	Enabled       bool       `json:"enabled"`
	MeasurementID *string    `json:"ga4_measurement_id"`
	APISecretSet  bool       `json:"ga4_api_secret_set"`
	Domain        *string    `json:"plausible_domain"`
	BaseURL       string     `json:"plausible_base_url"`
	LastSentAt    *time.Time `json:"last_sent_at"`
	LastError     *string    `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	UpdatedAt     *time.Time `json:"updated_at"`
	UpdatedByName *string    `json:"updated_by_name"`
}

// Change is a save of the settings. Every field replaces the stored one, except APISecret: nil keeps
// the stored secret and a pointer to "" removes it.
type Change struct {
	Provider      string
	Enabled       bool
	MeasurementID string
	APISecret     *string
	Domain        string
	BaseURL       string
}

// Clean trims and checks a change, with a ThemeError validation_failed naming every bad field.
// allow lists "host:port" addresses a test may use over http.
func Clean(change Change, allow map[string]bool) (Change, error) {
	fields := map[string]string{}
	change.Provider = strings.ToLower(strings.TrimSpace(change.Provider))
	if change.Provider != GA4 && change.Provider != Plausible {
		fields["provider"] = "Choose ga4 (Google Analytics 4) or plausible."
	}
	change.MeasurementID = strings.ToUpper(strings.TrimSpace(change.MeasurementID))
	if change.MeasurementID != "" && !measurementID.MatchString(change.MeasurementID) {
		fields["ga4_measurement_id"] = "Paste the measurement ID of the web stream: G- and 4 to 16 letters or digits, for example G-ABC123XYZ9."
	}
	if change.APISecret != nil {
		secret := strings.TrimSpace(*change.APISecret)
		change.APISecret = &secret
		if secret != "" && !apiSecret.MatchString(secret) {
			fields["ga4_api_secret"] = "Paste the secret value of a Measurement Protocol API secret: 8 to 100 letters, digits, - or _."
		}
	}
	change.Domain = strings.ToLower(strings.TrimSpace(change.Domain))
	change.Domain = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(change.Domain, "https://"), "http://"), "/")
	if change.Domain != "" && (len(change.Domain) > 253 || !siteDomain.MatchString(change.Domain)) {
		fields["plausible_domain"] = "Type the site domain exactly as it is in Plausible, for example help.example.com."
	}
	base, err := CheckBaseURL(change.BaseURL, allow)
	if err != nil {
		fields["plausible_base_url"] = err.Error()
	}
	change.BaseURL = base
	if change.Enabled && change.Provider == GA4 && change.MeasurementID == "" {
		fields["ga4_measurement_id"] = "Sending to GA4 needs the measurement ID of the web stream."
	}
	if change.Enabled && change.Provider == Plausible && change.Domain == "" {
		fields["plausible_domain"] = "Sending to Plausible needs the site domain."
	}
	if len(fields) > 0 {
		return Change{}, &domain.ThemeError{Code: "validation_failed", Fields: fields}
	}
	return change, nil
}

// Clean is the package Clean with the addresses this Sender may reach over http; a nil Sender allows
// none.
func (s *Sender) Clean(change Change) (Change, error) {
	if s == nil {
		return Clean(change, nil)
	}
	return Clean(change, s.options.AllowDial)
}

// SecretMissing is the field error of an enabled GA4 target without an API secret.
func SecretMissing() error {
	return &domain.ThemeError{Code: "validation_failed", Fields: map[string]string{
		"ga4_api_secret": "Sending to GA4 needs a Measurement Protocol API secret: create one under the web stream in GA4 Admin."}}
}

// CheckBaseURL checks a Plausible base URL a team typed and returns it cleaned; "" is Plausible's
// hosted service. It must be https and must not name a private, loopback or link-local address;
// netguard also refuses a public name that resolves to one when the page views are sent.
func CheckBaseURL(raw string, allow map[string]bool) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		return DefaultPlausibleBase, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasSuffix(value, "?") || len(value) > 200 {
		return "", fmt.Errorf("type the address Plausible runs at, for example %s or https://stats.example.com", DefaultPlausibleBase)
	}
	if allow[parsed.Host] {
		return value, nil
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil && netguard.Blocked(ip) {
		return "", fmt.Errorf("%s is a private address; Colliedesk sends only to a Plausible on a public address", ip)
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return "", fmt.Errorf("%s is this server's own address; Colliedesk sends only to a Plausible on a public address", host)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("use an https address, so page views do not cross the network in clear")
	}
	return value, nil
}
