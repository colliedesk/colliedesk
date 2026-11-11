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

func TestConfigFromEnvSoloAndSmallProductsAreOptional(t *testing.T) {
	values := fullEnv()
	values["POLAR_PRODUCT_SOLO_MONTHLY"], values["POLAR_PRODUCT_SOLO_YEARLY"] = " prod-solo-m ", "prod-solo-y"
	values["POLAR_PRODUCT_SMALL_MONTHLY"], values["POLAR_PRODUCT_SMALL_YEARLY"] = "prod-small-m", "prod-small-y"
	config, err := polar.ConfigFromEnv(env(values))
	if err != nil {
		t.Fatal(err)
	}
	products := config.Products
	if products.SoloMonthly != "prod-solo-m" || products.SoloYearly != "prod-solo-y" || products.SmallMonthly != "prod-small-m" || products.SmallYearly != "prod-small-y" {
		t.Fatalf("products=%+v", products)
	}
	for product, want := range map[string]string{"prod-solo-m": "solo", "prod-solo-y": "solo", "prod-small-m": "small", "prod-small-y": "small", "prod-monthly": "team", "": ""} {
		if got := products.PlanOf(product); got != want {
			t.Errorf("PlanOf(%q)=%q, want %q", product, got, want)
		}
	}
}

func TestCheckoutPlansAndPlansOnSaleFollowTheConfiguredProducts(t *testing.T) {
	config, err := polar.ConfigFromEnv(env(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(config.CheckoutPlans(), ","); got != "team_monthly,team_yearly,founding" {
		t.Errorf("only the required products: checkout plans %s", got)
	}
	if got := strings.Join(config.PlansOnSale(), ","); got != "team,founding" {
		t.Errorf("only the required products: plans on sale %s", got)
	}
	values := fullEnv()
	for _, key := range []string{"SOLO_MONTHLY", "SOLO_YEARLY", "SMALL_MONTHLY", "SMALL_YEARLY", "TEAM25_MONTHLY", "TEAM25_YEARLY"} {
		values["POLAR_PRODUCT_"+key] = "prod-" + key
	}
	config, err = polar.ConfigFromEnv(env(values))
	if err != nil {
		t.Fatal(err)
	}
	want := "solo_monthly,solo_yearly,small_monthly,small_yearly,team_monthly,team_yearly,team25_monthly,team25_yearly,founding"
	if got := strings.Join(config.CheckoutPlans(), ","); got != want {
		t.Errorf("every product: checkout plans %s, want %s", got, want)
	}
	if got := strings.Join(config.PlansOnSale(), ","); got != "solo,small,team,team25,founding" {
		t.Errorf("every product: plans on sale %s", got)
	}
	delete(values, "POLAR_PRODUCT_SMALL_YEARLY")
	config, _ = polar.ConfigFromEnv(env(values))
	if got := strings.Join(config.PlansOnSale(), ","); got != "solo,team,team25,founding" {
		t.Errorf("Small with one product: plans on sale %s, want Small left out", got)
	}
}

func TestCheckoutPlanOfNamesThePlanAndPeriodAProductIsSoldAs(t *testing.T) {
	products := polar.Products{
		TeamMonthly: "prod-team-m", TeamYearly: "prod-team-y", Founding: "prod-founding",
		Team25Monthly: "prod-t25-m", Team25Yearly: "prod-t25-y", SoloMonthly: "prod-solo-m", SmallYearly: "prod-small-y",
		Retired: map[string]string{"prod-old-team": "team"},
	}
	for product, want := range map[string]string{
		"prod-team-m": "team_monthly", "prod-team-y": "team_yearly", "prod-founding": "founding",
		"prod-t25-m": "team25_monthly", "prod-t25-y": "team25_yearly", "prod-solo-m": "solo_monthly", "prod-small-y": "small_yearly",
		// A retired product may have sold at another price, and an unset or unknown id is not for sale here.
		"prod-old-team": "", "prod-elsewhere": "", "": "",
	} {
		if got := products.CheckoutPlanOf(product); got != want {
			t.Errorf("CheckoutPlanOf(%q)=%q, want %q", product, got, want)
		}
	}
	// An optional plan with no product configured must not claim an empty product id.
	if got := (polar.Products{TeamMonthly: "a", TeamYearly: "b", Founding: "c"}).CheckoutPlanOf(""); got != "" {
		t.Errorf("CheckoutPlanOf(\"\")=%q with unset optional products, want empty", got)
	}
}
