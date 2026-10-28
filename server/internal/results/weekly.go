package results

import "time"

// The weekly email goes out on Monday at this hour, in the workspace's timezone.
const weeklyHour = 8

func location(timezone string) *time.Location {
	if loc, err := time.LoadLocation(timezone); err == nil {
		return loc
	}
	return time.UTC
}

// WeekStart is the Monday 00:00 of now's week in the timezone, as an instant: the key that claims a
// week's email.
func WeekStart(now time.Time, timezone string) time.Time {
	local := now.In(location(timezone))
	days := (int(local.Weekday()) + 6) % 7
	return time.Date(local.Year(), local.Month(), local.Day()-days, 0, 0, 0, 0, local.Location()).UTC()
}

// weeklyAt is the moment the email of the week starting at start goes out.
func weeklyAt(start time.Time, timezone string) time.Time {
	local := start.In(location(timezone))
	return time.Date(local.Year(), local.Month(), local.Day(), weeklyHour, 0, 0, 0, local.Location()).UTC()
}

// NextWeekly is the next Monday 8:00 in the timezone after now.
func NextWeekly(now time.Time, timezone string) time.Time {
	start := WeekStart(now, timezone)
	if at := weeklyAt(start, timezone); at.After(now) {
		return at
	}
	local := start.In(location(timezone))
	return weeklyAt(time.Date(local.Year(), local.Month(), local.Day()+7, 0, 0, 0, 0, local.Location()).UTC(), timezone)
}
