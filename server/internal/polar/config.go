package polar

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// DefaultAPIURL is Polar production. Sandbox is https://sandbox-api.polar.sh.
const DefaultAPIURL = "https://api.polar.sh"

// Products holds the Polar product id sold for each paid plan.
type Products struct {
	TeamMonthly string
	TeamYearly  string
	Founding    string
	// The Team 25, Solo and Small products are optional; empty means the plan is not sold here.
	Team25Monthly string
	Team25Yearly  string
	SoloMonthly   string
	SoloYearly    string
	SmallMonthly  string
	SmallYearly   string
}

// PlanOf is the plan a Polar product buys, or "" when the product is not sold here. An unset
// product id never matches, so an unconfigured plan cannot claim a subscription.
func (p Products) PlanOf(productID string) string {
	if productID == "" {
		return ""
	}
	switch productID {
	case p.SoloMonthly, p.SoloYearly:
		return domain.PlanSolo
	case p.SmallMonthly, p.SmallYearly:
		return domain.PlanSmall
	case p.TeamMonthly, p.TeamYearly:
		return domain.PlanTeam
	case p.Team25Monthly, p.Team25Yearly:
		return domain.PlanTeam25
	case p.Founding:
		return domain.PlanFounding
	}
	return ""
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
// POLAR_PRODUCT_TEAM_YEARLY, POLAR_PRODUCT_FOUNDING and the optional POLAR_API_URL and
// POLAR_PRODUCT_{SOLO,SMALL,TEAM25}_{MONTHLY,YEARLY}. An optional plan without its products cannot be bought. None set means
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
	config.Products.Team25Monthly, config.Products.Team25Yearly = value("POLAR_PRODUCT_TEAM25_MONTHLY"), value("POLAR_PRODUCT_TEAM25_YEARLY")
	config.Products.SoloMonthly, config.Products.SoloYearly = value("POLAR_PRODUCT_SOLO_MONTHLY"), value("POLAR_PRODUCT_SOLO_YEARLY")
	config.Products.SmallMonthly, config.Products.SmallYearly = value("POLAR_PRODUCT_SMALL_MONTHLY"), value("POLAR_PRODUCT_SMALL_YEARLY")
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

// CheckoutPlans lists the checkout plan names this configuration can sell, in the order they are
// offered: from the smallest plan up, Founding last. An optional plan is listed per configured product.
func (c Config) CheckoutPlans() []string {
	p := c.Products
	var plans []string
	for _, offer := range []struct{ name, product string }{
		{"solo_monthly", p.SoloMonthly}, {"solo_yearly", p.SoloYearly},
		{"small_monthly", p.SmallMonthly}, {"small_yearly", p.SmallYearly},
		{"team_monthly", p.TeamMonthly}, {"team_yearly", p.TeamYearly},
		{"team25_monthly", p.Team25Monthly}, {"team25_yearly", p.Team25Yearly},
	} {
		if offer.product != "" {
			plans = append(plans, offer.name)
		}
	}
	return append(plans, "founding")
}

// PlansOnSale lists the plans a workspace can move to here: a plan with a monthly and a yearly
// product counts, and so does Founding. A seat refusal offers only these.
func (c Config) PlansOnSale() []string {
	p := c.Products
	var plans []string
	for _, plan := range []struct{ id, monthly, yearly string }{
		{domain.PlanSolo, p.SoloMonthly, p.SoloYearly},
		{domain.PlanSmall, p.SmallMonthly, p.SmallYearly},
		{domain.PlanTeam, p.TeamMonthly, p.TeamYearly},
		{domain.PlanTeam25, p.Team25Monthly, p.Team25Yearly},
	} {
		if plan.monthly != "" && plan.yearly != "" {
			plans = append(plans, plan.id)
		}
	}
	return append(plans, domain.PlanFounding)
}
