package domain

import (
	"fmt"
	"regexp"
	"time"
)

// BusinessDay is one weekday of a business calendar: open from From to To ("08:00"), in its timezone.
type BusinessDay struct {
	Day  string `json:"day"`
	Open bool   `json:"open"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Holiday is a date the business is closed ("2026-12-25").
type Holiday struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

// Weekdays are the days a business calendar names, Monday first.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// BusinessCalendar counts business time: the opening hours of each weekday in its timezone, without its
// holidays. Its methods are pure.
type BusinessCalendar struct {
	location *time.Location
	open     [7]window // by time.Weekday
	holidays map[string]bool
}

type window struct {
	open     bool
	from, to int // minutes after midnight
}

// CalendarError says which part of a calendar is wrong.
type CalendarError struct{ Field, Message string }

func (e *CalendarError) Error() string { return e.Field + ": " + e.Message }

// NewBusinessCalendar reads a calendar; a weekday it does not name is closed.
func NewBusinessCalendar(zone string, days []BusinessDay, holidays []Holiday) (BusinessCalendar, error) {
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return BusinessCalendar{}, &CalendarError{Field: "timezone", Message: "Use an IANA timezone such as Europe/Berlin."}
	}
	calendar := BusinessCalendar{location: location, holidays: map[string]bool{}}
	seen := map[string]bool{}
	for index, day := range days {
		weekday := -1
		for n, name := range Weekdays {
			if name == day.Day {
				weekday = (n + 1) % 7 // mon is time.Monday (1), sun is time.Sunday (0)
			}
		}
		field := fmt.Sprintf("hours.%d", index)
		if weekday < 0 || seen[day.Day] {
			return BusinessCalendar{}, &CalendarError{Field: field + ".day", Message: "Use each of mon to sun once."}
		}
		seen[day.Day] = true
		if !day.Open {
			continue
		}
		if !clockTime.MatchString(day.From) {
			return BusinessCalendar{}, &CalendarError{Field: field + ".from", Message: "Use a time such as 08:00."}
		}
		// A day open round the clock closes at 24:00.
		if !clockTime.MatchString(day.To) && day.To != "24:00" {
			return BusinessCalendar{}, &CalendarError{Field: field + ".to", Message: "Use a time such as 17:00, or 24:00 for midnight."}
		}
		from, to := minutes(day.From), minutes(day.To)
		if to <= from {
			return BusinessCalendar{}, &CalendarError{Field: field + ".to", Message: "Ends before it starts."}
		}
		calendar.open[weekday] = window{open: true, from: from, to: to}
	}
	for index, holiday := range holidays {
		if _, err := time.Parse("2006-01-02", holiday.Date); err != nil {
			return BusinessCalendar{}, &CalendarError{Field: fmt.Sprintf("holidays.%d.date", index), Message: "Use a date such as 2026-12-25."}
		}
		calendar.holidays[holiday.Date] = true
	}
	return calendar, nil
}

func minutes(clock string) int {
	var hours, mins int
	_, _ = fmt.Sscanf(clock, "%d:%d", &hours, &mins)
	return hours*60 + mins
}

// searchDays bounds how far ahead business time is looked for: a calendar open less than once in two
// years counts as never open.
const searchDays = 800

// openWindow is the business window of the local day that starts at midnight, if it is open.
func (c BusinessCalendar) openWindow(midnight time.Time) (time.Time, time.Time, bool) {
	day := c.open[midnight.Weekday()]
	if !day.open || c.holidays[midnight.Format("2006-01-02")] {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := midnight.Date()
	return time.Date(y, m, d, day.from/60, day.from%60, 0, 0, c.location), time.Date(y, m, d, day.to/60, day.to%60, 0, 0, c.location), true
}

func (c BusinessCalendar) midnight(at time.Time) time.Time {
	y, m, d := at.In(c.location).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, c.location)
}

// Add is the instant amount of business time after start; the zero time when the calendar is never
// open within reach.
func (c BusinessCalendar) Add(start time.Time, amount time.Duration) time.Time {
	if amount <= 0 {
		return start
	}
	day := c.midnight(start)
	for range searchDays {
		if from, to, ok := c.openWindow(day); ok && to.After(start) {
			if from.Before(start) {
				from = start
			}
			if span := to.Sub(from); span >= amount {
				return from.Add(amount)
			} else {
				amount -= span
			}
		}
		y, m, d := day.Date()
		day = time.Date(y, m, d+1, 0, 0, 0, 0, c.location)
	}
	return time.Time{}
}

// Between is the business time from start to end.
func (c BusinessCalendar) Between(start, end time.Time) time.Duration {
	var total time.Duration
	for day := c.midnight(start); day.Before(end); {
		if from, to, ok := c.openWindow(day); ok {
			if from.Before(start) {
				from = start
			}
			if to.After(end) {
				to = end
			}
			if to.After(from) {
				total += to.Sub(from)
			}
		}
		y, m, d := day.Date()
		day = time.Date(y, m, d+1, 0, 0, 0, 0, c.location)
	}
	return total
}

// Open says whether the calendar has business time within reach of from.
func (c BusinessCalendar) Open(from time.Time) bool {
	return !c.Add(from, time.Minute).IsZero()
}

// Location is the calendar's timezone.
func (c BusinessCalendar) Location() *time.Location { return c.location }
