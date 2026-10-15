package domain

import "time"

// A locked workspace is deleted RetentionYears after it locked, once its admins were told at least
// RetentionNoticeLead before.
const (
	RetentionYears      = 1
	RetentionNoticeLead = 30 * 24 * time.Hour
)

// LockedSince returns when the workspace lost access: the latest end among its trial and every
// subscription or complimentary grant that ever gave access. It reports false while the workspace
// has access (NewAccess is not locked). An abandoned checkout (incomplete) never gave access, so it
// does not move the date.
func LockedSince(subscriptions []Subscription, trialEndsAt, now time.Time) (time.Time, bool) {
	if !NewAccess(subscriptions, trialEndsAt, now).Locked {
		return time.Time{}, false
	}
	since := UTC(trialEndsAt)
	for _, subscription := range subscriptions {
		if subscription.Status == "incomplete" || subscription.Status == "incomplete_expired" {
			continue
		}
		if end := subscription.accessEnd(); end.After(since) && !end.After(now) {
			since = UTC(end)
		}
	}
	return since, true
}

// accessEnd is when a subscription that is not entitled stopped granting its plan.
func (s Subscription) accessEnd() time.Time {
	switch {
	case s.EndedAt != nil && s.CurrentPeriodEnd != nil && s.CurrentPeriodEnd.Before(*s.EndedAt):
		return *s.CurrentPeriodEnd
	case s.EndedAt != nil:
		return *s.EndedAt
	case s.Status == "canceled" && s.CurrentPeriodEnd != nil:
		return *s.CurrentPeriodEnd
	default:
		return s.ModifiedAt
	}
}

// RetentionDates returns when the deletion notice is due and when the workspace may be deleted.
func RetentionDates(lockedSince time.Time) (notice, deletion time.Time) {
	deletion = UTC(lockedSince).AddDate(RetentionYears, 0, 0)
	return deletion.Add(-RetentionNoticeLead), deletion
}
