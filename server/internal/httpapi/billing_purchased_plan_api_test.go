package httpapi_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

const productRetiredTeam = "prod-team-retired"

// purchasedPlan reads purchased_plan of the billing answer; "" is null.
func purchasedPlan(t *testing.T, f billingFixture, token string) string {
	t.Helper()
	if got := getBilling(t, f.api, token).PurchasedPlan; got != nil {
		return *got
	}
	return ""
}

func TestBillingNamesTheCheckoutPlanTheSubscriptionWasBoughtAs(t *testing.T) {
	for product, want := range map[string]string{
		productMonthly: "team_monthly", productYearly: "team_yearly", productFounding: "founding",
		productTeam25Monthly: "team25_monthly", productTeam25Yearly: "team25_yearly",
		productSoloMonthly: "solo_monthly", productSoloYearly: "solo_yearly",
		productSmallMonthly: "small_monthly", productSmallYearly: "small_yearly",
	} {
		t.Run(want, func(t *testing.T) {
			f := newBillingFixture(t)
			sub := activeTeam(f.workspaceA, "sub_"+product, time.Now())
			sub.Product = product
			f.mustDeliver(t, "msg_"+product, sub.event("subscription.active"))
			if got := purchasedPlan(t, f, f.adminA); got != want {
				t.Fatalf("purchased_plan=%q, want %q", got, want)
			}
			if other := purchasedPlan(t, f, f.adminB); other != "" {
				t.Fatalf("another workspace reads purchased_plan=%q, want null", other)
			}
		})
	}
}

func TestBillingFollowsASubscriptionThatMovesToAnotherPriceInThePortal(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	sub := activeTeam(f.workspaceA, "sub_moves", now)
	sub.Product = productTeam25Monthly
	f.mustDeliver(t, "msg_before", sub.event("subscription.active"))
	if got := purchasedPlan(t, f, f.adminA); got != "team25_monthly" {
		t.Fatalf("before the move purchased_plan=%q, want team25_monthly", got)
	}
	sub.Product, sub.ModifiedAt = productTeam25Yearly, now.Add(time.Hour)
	f.mustDeliver(t, "msg_after", sub.event("subscription.updated"))
	if got := purchasedPlan(t, f, f.adminA); got != "team25_yearly" {
		t.Fatalf("after the move purchased_plan=%q, want team25_yearly", got)
	}
}

func TestBillingNamesNoPriceWhenTheProductIsNotOnSaleOrTheStoredRowPredatesIt(t *testing.T) {
	f := newBillingFixtureWith(t, func(config *httpapi.Config) {
		config.Billing.Products.Retired = map[string]string{productRetiredTeam: "team"}
	})
	sub := activeTeam(f.workspaceA, "sub_retired", time.Now())
	sub.Product = productRetiredTeam
	f.mustDeliver(t, "msg_retired", sub.event("subscription.active"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team" || got.PurchasedPlan != nil {
		t.Fatalf("billing=%+v, want team with purchased_plan null: a retired product may have sold at another price", got)
	}

	// A row written before the product id was kept.
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,current_period_end,source_modified_at)
		VALUES($1::uuid,'sub_legacy','founding','cus_legacy','active',now() + interval '20 days',now())`, f.workspaceB); err != nil {
		t.Fatal(err)
	}
	if got := getBilling(t, f.api, f.adminB); got.Plan != "founding" || got.PurchasedPlan != nil {
		t.Fatalf("billing=%+v, want founding with purchased_plan null for a row with no product id", got)
	}
}

func TestBillingNamesNoPriceForAComplimentaryPlanOrASubscriptionThatEnded(t *testing.T) {
	f := newBillingFixture(t)
	f.grantComp(t, f.workspaceA, nil)
	if got := getBilling(t, f.api, f.adminA); !got.Complimentary || got.PurchasedPlan != nil {
		t.Fatalf("billing=%+v, want complimentary with purchased_plan null", got)
	}

	now := time.Now().UTC().Truncate(time.Second)
	ended := now.Add(-time.Hour)
	sub := activeTeam(f.workspaceB, "sub_ended", now.Add(-48*time.Hour))
	sub.Product, sub.Status, sub.EndedAt, sub.PeriodEnd = productTeam25Yearly, "canceled", &ended, ended
	f.mustDeliver(t, "msg_ended", sub.event("subscription.canceled"))
	if got := getBilling(t, f.api, f.adminB); got.Plan == "team25" || got.PurchasedPlan != nil {
		t.Fatalf("billing=%+v, want no paid plan and purchased_plan null once the subscription ended", got)
	}
}

func TestBillingStillNamesThePriceWhileBillingIsSwitchedOff(t *testing.T) {
	f := newBillingFixtureWith(t, func(config *httpapi.Config) {
		config.SwitchedOff = map[string]bool{serverconfig.FeatureBilling: true}
	})
	sub := activeTeam(f.workspaceA, "sub_off", time.Now())
	sub.Product = productTeam25Yearly
	f.mustDeliver(t, "msg_off", sub.event("subscription.active"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "team25" || got.PurchasedPlan == nil || *got.PurchasedPlan != "team25_yearly" || len(got.CheckoutPlans) != 0 {
		t.Fatalf("billing=%+v, want team25 paid yearly with nothing for sale", got)
	}
}
