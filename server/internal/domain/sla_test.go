package domain_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The values of a contract like the one the plan names: first response in 4 business hours for every
// priority, resolution in 9 for urgent only, warned an hour before, Monday to Friday 08:00-17:00.
func contract(t *testing.T) domain.SLAPolicy {
	t.Helper()
	targets := map[string]domain.SLATarget{}
	for _, priority := range []string{"low", "normal", "high"} {
		targets[priority] = domain.SLATarget{FirstResponse: 4 * time.Hour}
	}
	targets["urgent"] = domain.SLATarget{FirstResponse: 4 * time.Hour, Resolution: 9 * time.Hour}
	return domain.SLAPolicy{Calendar: weekdays(t, "Asia/Bangkok"), Targets: targets, WarnBefore: time.Hour}
}

func TestAConversationsClocksRunWhileItIsOpen(t *testing.T) {
	const zone = "Asia/Bangkok"
	policy := contract(t)
	opened := at(t, zone, "2026-11-02 15:00")
	facts := domain.SLAFacts{CreatedAt: opened, Priority: "urgent", Changes: []domain.StatusAt{{At: opened, Status: "open"}}}
	first, resolution := policy.Evaluate(facts)
	if first == nil || first.State != domain.ClockRunning || !first.DueAt.Equal(at(t, zone, "2026-11-03 10:00")) || !first.WarnAt.Equal(at(t, zone, "2026-11-03 09:00")) {
		t.Fatalf("first response = %+v", first)
	}
	if resolution == nil || !resolution.DueAt.Equal(at(t, zone, "2026-11-03 15:00")) {
		t.Fatalf("resolution = %+v", resolution)
	}
	// A warning an hour before a due time early in the morning falls the evening before.
	morning := domain.SLAFacts{CreatedAt: at(t, zone, "2026-11-06 13:30"), Priority: "low", Changes: []domain.StatusAt{{At: at(t, zone, "2026-11-06 13:30"), Status: "open"}}}
	if first, _ := policy.Evaluate(morning); !first.DueAt.Equal(at(t, zone, "2026-11-09 08:30")) || !first.WarnAt.Equal(at(t, zone, "2026-11-06 16:30")) {
		t.Fatalf("a Monday morning due = %+v", first)
	}
	// A lead as long as the target, or longer, makes it due soon from the start.
	long := contract(t)
	long.WarnBefore = 4 * time.Hour
	if first, _ := long.Evaluate(domain.SLAFacts{CreatedAt: opened, Priority: "low", Changes: []domain.StatusAt{{At: opened, Status: "open"}}}); !first.WarnAt.Equal(opened) {
		t.Fatalf("a lead of the whole target warns at %s, want when it opened", first.WarnAt)
	}
	// Only urgent has a resolution target.
	facts.Priority = "normal"
	if _, resolution := policy.Evaluate(facts); resolution != nil {
		t.Fatalf("a normal conversation has a resolution clock: %+v", resolution)
	}
	// The first reply meets the first-response target.
	replied := at(t, zone, "2026-11-03 09:15")
	facts.FirstReplyAt = &replied
	if first, _ := policy.Evaluate(facts); first.State != domain.ClockMet || !first.MetAt.Equal(replied) {
		t.Fatalf("after the reply = %+v", first)
	}
}

// Pending stops the clock and keeps the business time used; resolved or closed stops it; opening again
// goes on from what was used.
func TestPendingPausesAndReopeningResumesTheClock(t *testing.T) {
	const zone = "Asia/Bangkok"
	policy := contract(t)
	opened := at(t, zone, "2026-11-02 09:00")
	changes := []domain.StatusAt{{At: opened, Status: "open"}, {At: at(t, zone, "2026-11-02 10:00"), Status: "pending"}}
	facts := domain.SLAFacts{CreatedAt: opened, Priority: "urgent", Changes: changes}
	if first, resolution := policy.Evaluate(facts); first.State != domain.ClockPaused || resolution.State != domain.ClockPaused || first.DueAt != nil {
		t.Fatalf("pending = %+v %+v", first, resolution)
	}
	// Open again a day later: an hour was used, three are left.
	facts.Changes = append(changes, domain.StatusAt{At: at(t, zone, "2026-11-03 14:00"), Status: "open"})
	if first, _ := policy.Evaluate(facts); first.State != domain.ClockRunning || !first.DueAt.Equal(at(t, zone, "2026-11-03 17:00")) {
		t.Fatalf("reopened = %+v", first)
	}
	facts.Changes = append(facts.Changes, domain.StatusAt{At: at(t, zone, "2026-11-03 15:00"), Status: "resolved"})
	if first, resolution := policy.Evaluate(facts); first.State != domain.ClockStopped || resolution.State != domain.ClockStopped {
		t.Fatalf("resolved = %+v %+v", first, resolution)
	}
	// A target already passed during an earlier open stretch stays where it fell.
	late := domain.SLAFacts{CreatedAt: opened, Priority: "low", Changes: []domain.StatusAt{{At: opened, Status: "open"},
		{At: at(t, zone, "2026-11-02 15:00"), Status: "pending"}, {At: at(t, zone, "2026-11-04 09:00"), Status: "open"}}}
	if first, _ := policy.Evaluate(late); !first.DueAt.Equal(at(t, zone, "2026-11-02 13:00")) {
		t.Fatalf("a target passed before the pause = %+v", first)
	}
	// One opened pending (a reply the team sent first) starts its clock when it opens.
	waiting := domain.SLAFacts{CreatedAt: opened, Priority: "low", Changes: []domain.StatusAt{{At: opened, Status: "pending"}, {At: at(t, zone, "2026-11-02 16:00"), Status: "open"}}}
	if first, _ := policy.Evaluate(waiting); !first.DueAt.Equal(at(t, zone, "2026-11-03 11:00")) {
		t.Fatalf("opened pending = %+v", first)
	}
}

func TestAClockSaysHowMuchBusinessTimeIsLeft(t *testing.T) {
	const zone = "Asia/Bangkok"
	policy := contract(t)
	opened := at(t, zone, "2026-11-02 09:00")
	first, _ := policy.Evaluate(domain.SLAFacts{CreatedAt: opened, Priority: "low", Changes: []domain.StatusAt{{At: opened, Status: "open"}}})
	if left := policy.MinutesLeft(first, at(t, zone, "2026-11-02 12:15")); left == nil || *left != 45 {
		t.Fatalf("left = %v", left)
	}
	if left := policy.MinutesLeft(first, at(t, zone, "2026-11-03 09:20")); left == nil || *left != -(5*60+20) {
		t.Fatalf("late = %v", left)
	}
}
