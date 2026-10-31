package domain_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func weekdays(t *testing.T, zone string, holidays ...string) domain.BusinessCalendar {
	t.Helper()
	hours := []domain.BusinessDay{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri"} {
		hours = append(hours, domain.BusinessDay{Day: day, Open: true, From: "08:00", To: "17:00"})
	}
	hours = append(hours, domain.BusinessDay{Day: "sat"}, domain.BusinessDay{Day: "sun"})
	var off []domain.Holiday
	for _, date := range holidays {
		off = append(off, domain.Holiday{Date: date, Name: "Off"})
	}
	calendar, err := domain.NewBusinessCalendar(zone, hours, off)
	if err != nil {
		t.Fatal(err)
	}
	return calendar
}

func at(t *testing.T, zone, value string) time.Time {
	t.Helper()
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// Business time runs Monday to Friday 08:00-17:00 in the calendar's timezone, skipping holidays.
func TestBusinessTimeIsAddedAndCountedOverOpeningHours(t *testing.T) {
	const zone = "Asia/Bangkok"
	calendar := weekdays(t, zone, "2026-11-04")
	for _, c := range []struct {
		from  string
		add   time.Duration
		want  string
		label string
	}{
		{"2026-11-02 09:00", 4 * time.Hour, "2026-11-02 13:00", "within a day"},
		{"2026-11-02 15:00", 4 * time.Hour, "2026-11-03 10:00", "over the night"},
		{"2026-11-02 06:30", time.Hour, "2026-11-02 09:00", "before opening"},
		{"2026-11-02 18:00", time.Hour, "2026-11-03 09:00", "after closing"},
		{"2026-11-02 17:00", time.Minute, "2026-11-03 08:01", "exactly at closing"},
		{"2026-11-06 16:00", 2 * time.Hour, "2026-11-09 09:00", "over the weekend"},
		{"2026-11-03 16:00", 2 * time.Hour, "2026-11-05 09:00", "over a holiday"},
		{"2026-11-02 08:00", 9 * time.Hour, "2026-11-02 17:00", "a whole day ends at closing"},
		{"2026-11-02 10:00", 0, "2026-11-02 10:00", "nothing to add"},
	} {
		if got := calendar.Add(at(t, zone, c.from), c.add); !got.Equal(at(t, zone, c.want)) {
			t.Fatalf("%s: %s + %s = %s, want %s", c.label, c.from, c.add, got.In(at(t, zone, c.want).Location()), c.want)
		}
	}
	if got := calendar.Between(at(t, zone, "2026-11-02 15:00"), at(t, zone, "2026-11-05 09:00")); got != 2*time.Hour+9*time.Hour+time.Hour {
		t.Fatalf("between = %s", got)
	}
	if got := calendar.Between(at(t, zone, "2026-11-07 10:00"), at(t, zone, "2026-11-08 10:00")); got != 0 {
		t.Fatalf("a weekend counts %s", got)
	}
}

// A timezone that moves its clocks counts the hours the people there work.
func TestBusinessTimeFollowsDaylightSaving(t *testing.T) {
	const zone = "Europe/Berlin"
	calendar := weekdays(t, zone)
	// 2026-10-25 is a Sunday: clocks went back. Friday 16:00 + 2 h is Monday 09:00 local.
	if got := calendar.Add(at(t, zone, "2026-10-23 16:00"), 2*time.Hour); !got.Equal(at(t, zone, "2026-10-26 09:00")) {
		t.Fatalf("over the change = %s", got)
	}
}

func TestABusinessCalendarRefusesWhatItCannotCount(t *testing.T) {
	for _, c := range []struct {
		zone  string
		hours []domain.BusinessDay
	}{
		{"Nowhere/City", nil},
		{"UTC", []domain.BusinessDay{{Day: "mon", Open: true, From: "17:00", To: "08:00"}}},
		{"UTC", []domain.BusinessDay{{Day: "mon", Open: true, From: "8", To: "17:00"}}},
		{"UTC", []domain.BusinessDay{{Day: "xyz", Open: true, From: "08:00", To: "17:00"}}},
		{"UTC", []domain.BusinessDay{{Day: "mon", Open: true, From: "08:00", To: "17:00"}, {Day: "mon"}}},
	} {
		if _, err := domain.NewBusinessCalendar(c.zone, c.hours, nil); err == nil {
			t.Fatalf("%s %+v was taken", c.zone, c.hours)
		}
	}
	if _, err := domain.NewBusinessCalendar("UTC", nil, []domain.Holiday{{Date: "2026-13-40"}}); err == nil {
		t.Fatal("a holiday that is no date was taken")
	}
	// No open day at all: business time never passes, and adding stops instead of looping forever.
	closed, err := domain.NewBusinessCalendar("UTC", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := closed.Add(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), time.Hour); !got.IsZero() {
		t.Fatalf("a closed calendar reached %s", got)
	}
}
