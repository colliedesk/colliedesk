package domain

import "time"

// Clock states of a response target.
const (
	ClockRunning = "running"
	ClockPaused  = "paused"
	ClockStopped = "stopped"
	ClockMet     = "met"
)

// SLATarget is how much business time a priority's conversations have; zero is no target.
type SLATarget struct {
	FirstResponse time.Duration
	Resolution    time.Duration
}

// SLAPolicy is a workspace's response targets over its business calendar.
type SLAPolicy struct {
	Calendar   BusinessCalendar
	Targets    map[string]SLATarget
	WarnBefore time.Duration
}

// StatusAt is a conversation's status from an instant on.
type StatusAt struct {
	At     time.Time
	Status string
}

// SLAFacts is what a conversation's clocks are worked out from: its statuses in order (the first is the
// one it was opened with), its first reply and its priority.
type SLAFacts struct {
	CreatedAt    time.Time
	Changes      []StatusAt
	FirstReplyAt *time.Time
	Priority     string
}

// SLAClock is one target's clock. DueAt and WarnAt are set while it runs, MetAt once a first response met
// it.
type SLAClock struct {
	State  string
	DueAt  *time.Time
	WarnAt *time.Time
	MetAt  *time.Time
}

type openStretch struct {
	start, end time.Time
	running    bool // the stretch is still open now
}

func stretches(facts SLAFacts) []openStretch {
	var open []openStretch
	for index, change := range facts.Changes {
		if change.Status != "open" {
			continue
		}
		if index+1 < len(facts.Changes) {
			open = append(open, openStretch{start: change.At, end: facts.Changes[index+1].At})
		} else {
			open = append(open, openStretch{start: change.At, running: true})
		}
	}
	return open
}

// walk spends target over the open stretches in order: the instant it is used up, even in an earlier
// stretch; false when the closed stretches never use it up.
func (p SLAPolicy) walk(open []openStretch, target time.Duration) (time.Time, bool) {
	left := target
	for _, stretch := range open {
		if stretch.running {
			due := p.Calendar.Add(stretch.start, left)
			return due, !due.IsZero()
		}
		used := p.Calendar.Between(stretch.start, stretch.end)
		if used >= left {
			return p.Calendar.Add(stretch.start, left), true
		}
		left -= used
	}
	return time.Time{}, false
}

func (p SLAPolicy) clock(open []openStretch, status string, target time.Duration) *SLAClock {
	switch status {
	case "open":
		clock := &SLAClock{State: ClockRunning}
		if due, ok := p.walk(open, target); ok {
			clock.DueAt = &due
			// Due soon from WarnBefore of business time before the target: from the start when the lead is
			// as long as the target or longer.
			warn := due
			if p.WarnBefore > 0 {
				if at, ok := p.walk(open, max(target-p.WarnBefore, 0)); ok {
					warn = at
				}
			}
			clock.WarnAt = &warn
		}
		return clock
	case "pending":
		return &SLAClock{State: ClockPaused}
	default:
		return &SLAClock{State: ClockStopped}
	}
}

// Evaluate works out a conversation's clocks; nil for a target its priority does not have. It never reads
// the time: the same facts give the same clocks.
func (p SLAPolicy) Evaluate(facts SLAFacts) (firstResponse, resolution *SLAClock) {
	target := p.Targets[facts.Priority]
	open := stretches(facts)
	status := "open"
	if n := len(facts.Changes); n > 0 {
		status = facts.Changes[n-1].Status
	}
	if target.FirstResponse > 0 {
		if facts.FirstReplyAt != nil {
			met := facts.FirstReplyAt.UTC()
			firstResponse = &SLAClock{State: ClockMet, MetAt: &met}
		} else {
			firstResponse = p.clock(open, status, target.FirstResponse)
		}
	}
	if target.Resolution > 0 {
		resolution = p.clock(open, status, target.Resolution)
	}
	for _, clock := range []*SLAClock{firstResponse, resolution} {
		if clock != nil {
			for _, at := range []*time.Time{clock.DueAt, clock.WarnAt} {
				if at != nil {
					*at = at.UTC()
				}
			}
		}
	}
	return firstResponse, resolution
}

// MinutesLeft is the business time from now to a running clock's due time, negative once it passed.
func (p SLAPolicy) MinutesLeft(clock *SLAClock, now time.Time) *int {
	if clock == nil || clock.State != ClockRunning || clock.DueAt == nil {
		return nil
	}
	var minutes int
	if now.Before(*clock.DueAt) {
		minutes = int(p.Calendar.Between(now, *clock.DueAt) / time.Minute)
	} else {
		minutes = -int(p.Calendar.Between(*clock.DueAt, now) / time.Minute)
	}
	return &minutes
}
