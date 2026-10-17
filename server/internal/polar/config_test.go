package polar_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func fullEnv() map[string]string {
	return map[string]string{
		"POLAR_ACCESS_TOKEN": "polar_oat_secret-token", "POLAR_WEBHOOK_SECRET": "whsec_hook-secret",
		"POLAR_PRODUCT_TEAM_MONTHLY": "prod-monthly", "POLAR_PRODUCT_TEAM_YEARLY": "prod-yearly", "POLAR_PRODUCT_FOUNDING": "prod-founding",
	}
}

func TestConfigFromEnvDisabledWhenNothingIsSet(t *testing.T) {
	config, err := polar.ConfigFromEnv(env(nil))
	if err != nil || config.Enabled() {
		t.Fatalf("enabled=%v err=%v, want disabled without error", config.Enabled(), err)
	}
}

func TestConfigFromEnvAllSet(t *testing.T) {
	config, err := polar.ConfigFromEnv(env(fullEnv()))
	if err != nil || !config.Enabled() {
		t.Fatalf("enabled=%v err=%v", config.Enabled(), err)
	}
	if config.APIURL != "https://api.polar.sh" {
		t.Fatalf("default API URL=%q, want production", config.APIURL)
	}
	if config.Products.TeamMonthly != "prod-monthly" || config.Products.TeamYearly != "prod-yearly" || config.Products.Founding != "prod-founding" {
		t.Fatalf("products=%+v", config.Products)
	}
}

func TestConfigFromEnvTeam25ProductsAreOptional(t *testing.T) {
	config, err := polar.ConfigFromEnv(env(fullEnv()))
	if err != nil || !config.Enabled() || config.Products.Team25Monthly != "" || config.Products.Team25Yearly != "" {
		t.Fatalf("without the Team 25 products: err=%v products=%+v, want billing on and both empty", err, config.Products)
	}
	values := fullEnv()
	values["POLAR_PRODUCT_TEAM25_MONTHLY"], values["POLAR_PRODUCT_TEAM25_YEARLY"] = " prod-t25-monthly ", "prod-t25-yearly"
	config, err = polar.ConfigFromEnv(env(values))
	if err != nil || config.Products.Team25Monthly != "prod-t25-monthly" || config.Products.Team25Yearly != "prod-t25-yearly" {
		t.Fatalf("with the Team 25 products: err=%v products=%+v", err, config.Products)
	}
}

func TestConfigFromEnvTeam25ProductsAloneDoNotTurnBillingOn(t *testing.T) {
	config, err := polar.ConfigFromEnv(env(map[string]string{"POLAR_PRODUCT_TEAM25_MONTHLY": "x"}))
	if err == nil && config.Enabled() {
		t.Fatal("a Team 25 product alone enabled billing")
	}
}

func TestConfigFromEnvSandboxURLAndTrailingSlash(t *testing.T) {
	values := fullEnv()
	values["POLAR_API_URL"] = "https://sandbox-api.polar.sh/"
	config, err := polar.ConfigFromEnv(env(values))
	if err != nil || config.APIURL != "https://sandbox-api.polar.sh" {
		t.Fatalf("url=%q err=%v", config.APIURL, err)
	}
}

func TestConfigFromEnvPartialNamesMissingKeysWithoutValues(t *testing.T) {
	for _, missing := range []string{"POLAR_ACCESS_TOKEN", "POLAR_WEBHOOK_SECRET", "POLAR_PRODUCT_TEAM_MONTHLY", "POLAR_PRODUCT_TEAM_YEARLY", "POLAR_PRODUCT_FOUNDING"} {
		values := fullEnv()
		delete(values, missing)
		_, err := polar.ConfigFromEnv(env(values))
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("missing %s: err=%v, want an error naming it", missing, err)
		}
		for _, secret := range []string{"polar_oat_secret-token", "whsec_hook-secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks a configured value: %v", err)
			}
		}
	}
}

func TestConfigFromEnvAPIURLAloneIsPartial(t *testing.T) {
	if _, err := polar.ConfigFromEnv(env(map[string]string{"POLAR_API_URL": "https://sandbox-api.polar.sh"})); err == nil {
		t.Fatal("POLAR_API_URL alone must be rejected as partial configuration")
	}
}

func TestConfigFromEnvRejectsBadAPIURL(t *testing.T) {
	values := fullEnv()
	values["POLAR_API_URL"] = "not a url"
	if _, err := polar.ConfigFromEnv(env(values)); err == nil {
		t.Fatal("want an error for a malformed POLAR_API_URL")
	}
}
