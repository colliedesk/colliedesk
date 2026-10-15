package domain_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func TestLockedSinceIsTheLatestEndOfAnyAccess(t *testing.T) {
	trialEnd := instant("2026-11-01T00:00:00Z")
	endedAt := instant("2027-02-10T12:00:00Z")
	periodEnd := instant("2027-03-01T00:00:00Z")
	compEnd := instant("2027-05-01T00:00:00Z")
	cases := []struct {
		name string
		subs []domain.Subscription
		now  string
		want string // empty means not locked
	}{
		{"trial still running", nil, "2026-10-31T23:59:59Z", ""},
		{"trial only", nil, "2027-06-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"polar subscription ended after the trial", []domain.Subscription{{Plan: "team", Status: "canceled", EndedAt: &endedAt}}, "2027-06-01T00:00:00Z", "2027-02-10T12:00:00Z"},
		{"canceled at the end of its paid period", []domain.Subscription{{Plan: "team", Status: "canceled", CurrentPeriodEnd: &periodEnd}}, "2027-06-01T00:00:00Z", "2027-03-01T00:00:00Z"},
		{"complimentary grant ran out", []domain.Subscription{{Plan: "team", SubscriptionID: "comp:1", Status: "canceled", CurrentPeriodEnd: &compEnd}}, "2027-06-01T00:00:00Z", "2027-05-01T00:00:00Z"},
		{"the latest of several wins", []domain.Subscription{
			{Plan: "team", Status: "canceled", EndedAt: &endedAt},
			{Plan: "team", Status: "canceled", CurrentPeriodEnd: &periodEnd}}, "2027-06-01T00:00:00Z", "2027-03-01T00:00:00Z"},
		{"still entitled", []domain.Subscription{{Plan: "team", Status: "active"}}, "2027-06-01T00:00:00Z", ""},
		{"unpaid counts from its last change", []domain.Subscription{{Plan: "team", Status: "unpaid", ModifiedAt: instant("2027-04-04T00:00:00Z")}}, "2027-06-01T00:00:00Z", "2027-04-04T00:00:00Z"},
		{"an access end a few seconds ahead of our clock counts as now", []domain.Subscription{{Plan: "team", Status: "unpaid", ModifiedAt: instant("2027-06-01T00:00:02Z")}}, "2027-06-01T00:00:00Z", "2027-06-01T00:00:00Z"},
		{"a scheduled end in the future counts as now", []domain.Subscription{{Plan: "team", Status: "unpaid", EndedAt: ptr(instant("2027-07-01T00:00:00Z"))}}, "2027-06-01T00:00:00Z", "2027-06-01T00:00:00Z"},
		{"an abandoned checkout never had access", []domain.Subscription{{Plan: "team", Status: "incomplete", ModifiedAt: instant("2027-04-04T00:00:00Z")}}, "2027-06-01T00:00:00Z", "2026-11-01T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, locked := domain.LockedSince(c.subs, trialEnd, instant(c.now))
			if c.want == "" {
				if locked {
					t.Fatalf("locked since %s, want not locked", got)
				}
				return
			}
			if !locked || !got.Equal(instant(c.want)) {
				t.Fatalf("LockedSince = %s, %v; want %s", got, locked, c.want)
			}
		})
	}
}

func TestRetentionDatesFollowTheCalendarYear(t *testing.T) {
	notice, deletion := domain.RetentionDates(instant("2026-11-01T00:00:00Z"))
	if !notice.Equal(instant("2027-10-02T00:00:00Z")) || !deletion.Equal(instant("2027-11-01T00:00:00Z")) {
		t.Fatalf("notice %s deletion %s, want 2027-10-02 and 2027-11-01", notice, deletion)
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestRetentionFloorNeverPrecedesTheFeatureRelease(t *testing.T) {
	if got := domain.RetentionReleasedAt; !got.Equal(instant("2026-10-16T00:00:00Z")) {
		t.Fatalf("RetentionReleasedAt = %s", got)
	}
}

func TestRetentionLockStartHonoursTheFloors(t *testing.T) {
	cases := []struct{ name, locked, change, want string }{
		{"normal lock after the release", "2026-11-01T00:00:00Z", "2026-10-20T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"trial end backdated to 2020 by an operator on 2027-01-05", "2020-01-01T00:00:00Z", "2027-01-05T10:00:00Z", "2027-01-05T10:00:00Z"},
		{"old trial end, no change recorded", "2025-01-01T00:00:00Z", "0001-01-01T00:00:00Z", "2026-10-16T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.RetentionLockStart(instant(c.locked), instant(c.change)); !got.Equal(instant(c.want)) {
				t.Fatalf("RetentionLockStart = %s, want %s", got, c.want)
			}
		})
	}
}
