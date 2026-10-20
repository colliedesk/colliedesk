package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Byte counts are written out by hand: 10 GiB, 30 GiB, 100 GiB, 250 GiB, 20 GiB and 80% of each.
func TestStorageCapPerPlan(t *testing.T) {
	cases := []struct {
		plan string
		cap  int64
		warn int64
	}{
		{domain.PlanSolo, 10737418240, 8589934592},
		{domain.PlanSmall, 32212254720, 25769803776},
		{domain.PlanTeam, 107374182400, 85899345920},
		{domain.PlanTeam25, 268435456000, 214748364800},
		{domain.PlanFounding, 21474836480, 17179869184},
	}
	for _, c := range cases {
		if got := domain.StorageCap(c.plan); got != c.cap {
			t.Errorf("StorageCap(%s)=%d, want %d", c.plan, got, c.cap)
		}
		limits := domain.LimitsFor(c.plan)
		if limits.StorageBytes == nil || *limits.StorageBytes != c.cap || limits.StorageWarnBytes == nil || *limits.StorageWarnBytes != c.warn {
			t.Errorf("LimitsFor(%s) storage=%v warn=%v, want %d and %d", c.plan, limits.StorageBytes, limits.StorageWarnBytes, c.cap, c.warn)
		}
	}
}

func TestStorageCountsImportsOnEveryPaidPlan(t *testing.T) {
	for plan, want := range map[string]bool{domain.PlanSolo: true, domain.PlanSmall: true, domain.PlanTeam: true, domain.PlanTeam25: true, domain.PlanFounding: true, domain.PlanTrial: false, domain.PlanNone: false} {
		if got := domain.StorageCountsImports(plan); got != want {
			t.Errorf("StorageCountsImports(%s)=%v, want %v", plan, got, want)
		}
	}
}

func TestAgentCapPerPlan(t *testing.T) {
	for plan, want := range map[string]int{domain.PlanSolo: 1, domain.PlanSmall: 3, domain.PlanTrial: 10, domain.PlanTeam: 10, domain.PlanFounding: 10, domain.PlanTeam25: 25, domain.PlanNone: 0} {
		if got := domain.LimitsFor(plan).Agents; got != want {
			t.Errorf("LimitsFor(%s).Agents=%d, want %d", plan, got, want)
		}
	}
}

func TestTeam25OutranksOtherPaidPlans(t *testing.T) {
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	subs := []domain.Subscription{
		{Plan: domain.PlanFounding, SubscriptionID: "sub_f", Status: "active", CurrentPeriodEnd: &future},
		{Plan: domain.PlanTeam25, SubscriptionID: "sub_t25", Status: "active", CurrentPeriodEnd: &future},
		{Plan: domain.PlanTeam, SubscriptionID: "sub_t", Status: "active", CurrentPeriodEnd: &future},
	}
	got := domain.NewAccess(subs, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)).Plan
	if got != domain.PlanTeam25 {
		t.Fatalf("plan in effect=%s, want team25", got)
	}
}

func TestPaidPlanRankFollowsTheLimitsTheyGive(t *testing.T) {
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	held := func(plans ...string) string {
		var subs []domain.Subscription
		for i, plan := range plans {
			subs = append(subs, domain.Subscription{Plan: plan, SubscriptionID: "sub_" + string(rune('a'+i)), Status: "active", CurrentPeriodEnd: &future})
		}
		return domain.NewAccess(subs, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)).Plan
	}
	if got := held(domain.PlanFounding, domain.PlanTeam); got != domain.PlanTeam {
		t.Errorf("Founding and Team held together: %s, want team (100 GiB beats 20 GiB at the same agents)", got)
	}
	if got := held(domain.PlanTeam, domain.PlanFounding); got != domain.PlanTeam {
		t.Errorf("Team and Founding held together, other order: %s, want team", got)
	}
	if got := held(domain.PlanTeam, domain.PlanTeam25, domain.PlanFounding); got != domain.PlanTeam25 {
		t.Errorf("all three held: %s, want team25", got)
	}
}

func TestPlanNamesOfTheLadder(t *testing.T) {
	for plan, want := range map[string]string{domain.PlanSolo: "Solo", domain.PlanSmall: "Small", domain.PlanTeam: "Team", domain.PlanTeam25: "Team 25", domain.PlanFounding: "Founding"} {
		if got := domain.PlanName(plan); got != want {
			t.Errorf("PlanName(%s)=%q, want %q", plan, got, want)
		}
	}
}

func TestSmallerPlansRankBelowFoundingAndTeam(t *testing.T) {
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	held := func(plans ...string) string {
		var subs []domain.Subscription
		for i, plan := range plans {
			subs = append(subs, domain.Subscription{Plan: plan, SubscriptionID: "sub_" + string(rune('a'+i)), Status: "active", CurrentPeriodEnd: &future})
		}
		return domain.NewAccess(subs, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)).Plan
	}
	cases := []struct {
		plans []string
		want  string
	}{
		{[]string{domain.PlanSmall, domain.PlanSolo}, domain.PlanSmall},
		{[]string{domain.PlanSolo, domain.PlanSmall}, domain.PlanSmall},
		{[]string{domain.PlanSmall, domain.PlanFounding}, domain.PlanFounding},
		{[]string{domain.PlanTeam, domain.PlanSmall}, domain.PlanTeam},
	}
	for _, c := range cases {
		if got := held(c.plans...); got != c.want {
			t.Errorf("%v held together: %s, want %s", c.plans, got, c.want)
		}
	}
}

func TestUpgradeIsTheNextPlanUpThatIsOnSale(t *testing.T) {
	everything := []string{domain.PlanSolo, domain.PlanSmall, domain.PlanTeam, domain.PlanTeam25, domain.PlanFounding}
	cases := []struct {
		plan   string
		onSale []string
		want   string
	}{
		{domain.PlanSolo, everything, domain.PlanSmall},
		{domain.PlanSmall, everything, domain.PlanTeam},
		{domain.PlanTeam, everything, domain.PlanTeam25},
		{domain.PlanTeam25, everything, ""},
		{domain.PlanTeam, []string{domain.PlanTeam, domain.PlanFounding}, ""},
		{domain.PlanSolo, []string{domain.PlanSolo, domain.PlanTeam}, domain.PlanTeam},
		{domain.PlanFounding, everything, ""},
		{domain.PlanTrial, everything, ""},
	}
	for _, c := range cases {
		if got := domain.UpgradeFor(c.plan, c.onSale); got != c.want {
			t.Errorf("UpgradeFor(%s, %v)=%q, want %q", c.plan, c.onSale, got, c.want)
		}
	}
}

func TestPlanLimitErrorPointsToTheNextTier(t *testing.T) {
	cases := []struct {
		err  *domain.PlanLimitError
		want string
	}{
		{&domain.PlanLimitError{Plan: domain.PlanSolo, Agents: 1, Upgrade: domain.PlanSmall},
			"The Solo plan allows 1 agent, pending invites included. Revoke a pending invite to free a seat, or upgrade to Small (up to 3 agents) under Manage billing."},
		{&domain.PlanLimitError{Plan: domain.PlanSmall, Agents: 3, Upgrade: domain.PlanTeam},
			"The Small plan allows 3 agents, pending invites included. Revoke a pending invite to free a seat, or upgrade to Team (up to 10 agents) under Manage billing."},
		{&domain.PlanLimitError{Plan: domain.PlanTeam, Agents: 10, Upgrade: domain.PlanTeam25},
			"The Team plan allows 10 agents, pending invites included. Revoke a pending invite to free a seat, or upgrade to Team 25 (up to 25 agents) under Manage billing."},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.err, got, c.want)
		}
	}
}

func TestPlanLimitErrorWithoutAnUpgradeSaysContactUs(t *testing.T) {
	for _, e := range []*domain.PlanLimitError{
		{Plan: domain.PlanTeam, Agents: 10},
		{Plan: domain.PlanFounding, Agents: 10},
		{Plan: domain.PlanTeam25, Agents: 25},
	} {
		if message := e.Error(); strings.Contains(message, "upgrade to") || !strings.Contains(message, "contact us") {
			t.Errorf("%+v: %q, want only the contact-us wording", e, message)
		}
	}
}

func TestStorageUpgradeIsTheNextPlanOnSaleWithMoreStorageAndAsManyAgents(t *testing.T) {
	everything := []string{domain.PlanSolo, domain.PlanSmall, domain.PlanTeam, domain.PlanTeam25, domain.PlanFounding}
	cases := []struct {
		plan   string
		onSale []string
		want   string
	}{
		{domain.PlanSolo, everything, domain.PlanSmall},
		{domain.PlanSmall, everything, domain.PlanTeam},
		{domain.PlanFounding, everything, domain.PlanTeam},
		{domain.PlanTeam, everything, domain.PlanTeam25},
		{domain.PlanTeam25, everything, ""},
		{domain.PlanSmall, []string{domain.PlanTeam25}, domain.PlanTeam25},
		{domain.PlanFounding, []string{domain.PlanSmall, domain.PlanFounding}, ""},
		{domain.PlanSolo, nil, ""},
	}
	for _, c := range cases {
		if got := domain.StorageUpgradeFor(c.plan, c.onSale); got != c.want {
			t.Errorf("StorageUpgradeFor(%s, %v)=%q, want %q", c.plan, c.onSale, got, c.want)
		}
	}
}

func TestStorageLimitErrorNamesTheUpgradeItIsGiven(t *testing.T) {
	message := (&domain.StorageLimitError{Plan: domain.PlanSolo, Limit: 10 << 30, Upgrade: domain.PlanSmall}).Error()
	if !strings.Contains(message, "limit of the Solo plan") || !strings.Contains(message, "Upgrade to the Small plan under Billing > Manage billing") {
		t.Errorf("Solo with Small on sale: %q", message)
	}
	for _, e := range []*domain.StorageLimitError{
		{Plan: domain.PlanSolo, Limit: 10 << 30},
		{Plan: domain.PlanTeam25, Limit: 250 << 30},
	} {
		if message := e.Error(); strings.Contains(message, "Upgrade to") || !strings.Contains(message, "Contact us for more storage") {
			t.Errorf("%+v without an upgrade on sale: %q, want the contact-us wording", e, message)
		}
	}
}

func TestPlanFitRefusalNamesWhatToRemove(t *testing.T) {
	cases := []struct {
		plan        string
		seats       int
		storedBytes int64
		want        string
	}{
		{domain.PlanSolo, 1, 10 << 30, ""},
		{domain.PlanSolo, 3, 0, "The Solo plan allows 1 agent and this workspace uses 3 seats (agents and pending invites). Deactivate agents or revoke invites to free 2 seats, then choose Solo."},
		{domain.PlanSmall, 4, 0, "The Small plan allows 3 agents and this workspace uses 4 seats (agents and pending invites). Deactivate agents or revoke invites to free 1 seat, then choose Small."},
		{domain.PlanSolo, 1, 12 << 30, "The Solo plan stores up to 10 GiB of attachments and this workspace stores 12 GiB, imported files included. Choose a plan with more storage."},
		{domain.PlanTeam25, 25, 250 << 30, ""},
	}
	for _, c := range cases {
		if got := domain.PlanFitRefusal(c.plan, c.seats, c.storedBytes, domain.StorageCap(c.plan)); got != c.want {
			t.Errorf("PlanFitRefusal(%s, %d seats, %d bytes):\n got %q\nwant %q", c.plan, c.seats, c.storedBytes, got, c.want)
		}
	}
}

func TestHoldingFoundingAndSmallKeepsTheLargerOfEachLimit(t *testing.T) {
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	subs := []domain.Subscription{
		{Plan: domain.PlanFounding, SubscriptionID: "sub_f", Status: "active", CurrentPeriodEnd: &future},
		{Plan: domain.PlanSmall, SubscriptionID: "sub_s", Status: "active", CurrentPeriodEnd: &future},
	}
	access := domain.NewAccess(subs, now.Add(-time.Hour), now)
	if access.Plan != domain.PlanFounding || access.StorageLimit() != 32212254720 {
		t.Fatalf("plan=%s storage=%d, want founding (10 agents) with Small's 30 GiB (32212254720)", access.Plan, access.StorageLimit())
	}
	limits := domain.NewBilling(subs, now.Add(-time.Hour), domain.BillingUsage{}, now).Limits
	if limits.Agents != 10 || limits.StorageBytes == nil || *limits.StorageBytes != 32212254720 {
		t.Fatalf("billing limits=%+v, want 10 agents and 30 GiB", limits)
	}
	if single := domain.NewAccess(subs[:1], now.Add(-time.Hour), now); single.StorageLimit() != 21474836480 {
		t.Fatalf("Founding alone: storage=%d, want 20 GiB", single.StorageLimit())
	}
}

func TestSeatPausedErrorSaysWhyAndWhoCanFixIt(t *testing.T) {
	message := (&domain.SeatPausedError{Plan: domain.PlanSmall, Agents: 3}).Error()
	for _, want := range []string{"The Small plan allows 3 agents", "workspace admin", "deactivates agents or upgrades"} {
		if !strings.Contains(message, want) {
			t.Errorf("%q lacks %q", message, want)
		}
	}
}
