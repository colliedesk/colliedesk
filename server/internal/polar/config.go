package polar

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultAPIURL is Polar production. Sandbox is https://sandbox-api.polar.sh.
const DefaultAPIURL = "https://api.polar.sh"

// Products holds the Polar product id sold for each paid plan.
type Products struct {
	TeamMonthly string
	TeamYearly  string
	Founding    string
}

// Config enables billing. The zero value means billing is off (self-hosted).
type Config struct {
	AccessToken   string
	WebhookSecret string
	APIURL        string
	Products      Products
}

// Enabled reports whether billing is configured.
func (c Config) Enabled() bool { return c.AccessToken != "" }

// ConfigFromEnv reads POLAR_ACCESS_TOKEN, POLAR_WEBHOOK_SECRET, POLAR_PRODUCT_TEAM_MONTHLY,
// POLAR_PRODUCT_TEAM_YEARLY, POLAR_PRODUCT_FOUNDING and the optional POLAR_API_URL. None set means
// billing is off. Setting some but not all is an error that names the missing keys (never values),
// so a half-configured instance never starts.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	value := func(key string) string { return strings.TrimSpace(getenv(key)) }
	config := Config{APIURL: DefaultAPIURL}
	type field struct {
		key    string
		target *string
	}
	required := []field{
		{"POLAR_ACCESS_TOKEN", &config.AccessToken},
		{"POLAR_WEBHOOK_SECRET", &config.WebhookSecret},
		{"POLAR_PRODUCT_TEAM_MONTHLY", &config.Products.TeamMonthly},
		{"POLAR_PRODUCT_TEAM_YEARLY", &config.Products.TeamYearly},
		{"POLAR_PRODUCT_FOUNDING", &config.Products.Founding},
	}
	apiURL := value("POLAR_API_URL")
	var missing []string
	for _, item := range required {
		*item.target = value(item.key)
		if *item.target == "" {
			missing = append(missing, item.key)
		}
	}
	if len(missing) == len(required) && apiURL == "" {
		return Config{}, nil
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("billing is partly configured; also set %s (or unset every POLAR_* variable to turn billing off)", strings.Join(missing, ", "))
	}
	if apiURL != "" {
		parsed, err := url.Parse(apiURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return Config{}, fmt.Errorf("POLAR_API_URL must be an http(s) URL such as %s", DefaultAPIURL)
		}
		config.APIURL = strings.TrimRight(apiURL, "/")
	}
	return config, nil
}
