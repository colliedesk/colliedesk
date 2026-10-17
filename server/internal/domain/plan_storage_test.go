package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Byte counts are written out by hand: 100 GiB, 250 GiB, and 80% of each.
func TestStorageCapPerPlan(t *testing.T) {
	cases := []struct {
		plan string
		cap  int64
		warn int64
	}{
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
	for plan, want := range map[string]bool{domain.PlanTeam: true, domain.PlanTeam25: true, domain.PlanFounding: true, domain.PlanTrial: false, domain.PlanNone: false} {
		if got := domain.StorageCountsImports(plan); got != want {
			t.Errorf("StorageCountsImports(%s)=%v, want %v", plan, got, want)
		}
	}
}

func TestAgentCapPerPlan(t *testing.T) {
	for plan, want := range map[string]int{domain.PlanTrial: 10, domain.PlanTeam: 10, domain.PlanFounding: 10, domain.PlanTeam25: 25, domain.PlanNone: 0} {
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

func TestPlanLimitErrorPointsTeamToTeam25OnlyWhenItIsOffered(t *testing.T) {
	offered := (&domain.PlanLimitError{Plan: domain.PlanTeam, Agents: 10, Team25Offered: true}).Error()
	if !strings.Contains(offered, "upgrade to Team 25") || !strings.Contains(offered, "25 agents") {
		t.Errorf("Team with Team 25 on sale: %q, want a pointer to Team 25 and its 25 agents", offered)
	}
	for _, e := range []*domain.PlanLimitError{
		{Plan: domain.PlanTeam, Agents: 10},
		{Plan: domain.PlanFounding, Agents: 10, Team25Offered: true},
		{Plan: domain.PlanTeam25, Agents: 25, Team25Offered: true},
	} {
		if message := e.Error(); strings.Contains(message, "Team 25 under") || !strings.Contains(message, "contact us") {
			t.Errorf("%+v: %q, want only the contact-us wording", e, message)
		}
	}
}
