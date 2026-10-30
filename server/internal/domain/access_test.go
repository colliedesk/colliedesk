package domain_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func instant(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestNewAccessFollowsTrialAndSubscriptions(t *testing.T) {
	trialEnd := instant("2026-10-15T09:00:00Z")
	periodEnd := instant("2026-11-01T00:00:00Z")
	canceledTeam := domain.Subscription{Plan: domain.PlanTeam, Status: "canceled", CurrentPeriodEnd: &periodEnd}
	activeTeam := domain.Subscription{Plan: domain.PlanTeam, Status: "active"}
	cases := []struct {
		name       string
		subs       []domain.Subscription
		now        string
		wantPlan   string
		wantLocked bool
	}{
		{"one second before the trial ends", nil, "2026-10-15T08:59:59Z", "trial", false},
		{"at the exact trial end", nil, "2026-10-15T09:00:00Z", "none", true},
		{"long after the trial", nil, "2027-01-01T00:00:00Z", "none", true},
		{"paid beats the running trial", []domain.Subscription{activeTeam}, "2026-10-10T00:00:00Z", "team", false},
		{"active team after the trial", []domain.Subscription{activeTeam}, "2026-12-01T00:00:00Z", "team", false},
		{"canceled team until its period ends", []domain.Subscription{canceledTeam}, "2026-10-31T23:59:59Z", "team", false},
		{"canceled team at period end", []domain.Subscription{canceledTeam}, "2026-11-01T00:00:00Z", "none", true},
		{"canceled team back inside the trial", []domain.Subscription{canceledTeam}, "2026-10-01T00:00:00Z", "team", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := domain.NewAccess(c.subs, trialEnd, instant(c.now))
			if got.Plan != c.wantPlan || got.Locked != c.wantLocked || !got.TrialEndsAt.Equal(trialEnd) {
				t.Fatalf("access=%+v, want plan %s locked %v trial end %s", got, c.wantPlan, c.wantLocked, trialEnd)
			}
		})
	}
}

func TestLockedPlanHasNoSeatsAndTeamAndFoundingHaveTen(t *testing.T) {
	for plan, want := range map[string]int{domain.PlanTeam: 10, domain.PlanFounding: 10, domain.PlanNone: 0} {
		if got := domain.LimitsFor(plan).Agents; got != want {
			t.Fatalf("%s agents=%d, want %d", plan, got, want)
		}
	}
}

func TestExpiredComplimentaryGrantLeavesNoBillingStatusToManage(t *testing.T) {
	end := instant("2026-11-01T00:00:00Z")
	expired := domain.Subscription{Plan: domain.PlanTeam, SubscriptionID: domain.CompSubscriptionPrefix + "1", Status: "canceled", CurrentPeriodEnd: &end, ModifiedAt: instant("2026-10-20T09:00:00Z")}
	got := domain.NewBilling([]domain.Subscription{expired}, instant("2026-10-15T09:00:00Z"), domain.BillingUsage{}, instant("2026-12-01T00:00:00Z"))
	if got.Status != domain.BillingStatusNone || got.CurrentPeriodEnd != nil || got.Complimentary || got.Plan != domain.PlanNone || !got.Locked {
		t.Fatalf("billing=%+v, want a locked workspace with no subscription state (no portal to open)", got)
	}
}
