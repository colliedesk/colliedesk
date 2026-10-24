package results

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

var measured = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func ago(days float64) time.Time { return measured.Add(-time.Duration(days * float64(Day))) }

func after(at time.Time, d time.Duration) *time.Time {
	value := at.Add(d)
	return &value
}

func ptr[T any](value T) *T { return &value }

// conversation builds a live conversation created days ago, answered after reply (0: never).
func conversation(id string, days float64, reply time.Duration, changes ...StatusChange) Conversation {
	created := ago(days)
	c := Conversation{ID: id, CreatedAt: created, FirstIncoming: created, Changes: changes}
	if reply > 0 {
		c.FirstReply = after(created, reply)
	}
	return c
}

func change(from, to string, created time.Time, at time.Duration) StatusChange {
	return StatusChange{From: from, To: to, At: created.Add(at)}
}

func sorted(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

// Every expected value below was worked out by hand from the fixture (see the comments).
func TestLiveNumbersFollowTheCountingRules(t *testing.T) {
	f, g, h, i, k, l := ago(10), ago(12), ago(15), ago(20), ago(8), ago(9)
	conversations := []Conversation{
		conversation("a", 2, 30*time.Minute),
		conversation("b", 3, time.Hour),
		conversation("c", 4, 2*time.Hour),
		conversation("d", 5, 10*time.Minute),
		conversation("e", 6, 0),
		// Resolved a day after it came in, and stayed resolved.
		conversation("f", 10, 4*time.Hour, change("open", "resolved", f, Day)),
		// Resolved, reopened within the day, closed at 3 days: two resolutions, one reopen, resolved at 3 days.
		conversation("g", 12, 20*time.Minute, change("open", "resolved", g, 2*time.Hour), change("resolved", "open", g, Day), change("open", "closed", g, 3*Day)),
		conversation("h", 15, 5*time.Minute, change("open", "resolved", h, 5*Day)),
		// Resolved after an hour, then closed: closing a resolved conversation is not a new resolution.
		conversation("i", 20, 40*time.Minute, change("open", "resolved", i, time.Hour), change("resolved", "closed", i, 2*time.Hour)),
		conversation("j", 25, 90*time.Minute),
		// Resolved half a day after it came in, 7.5 days ago: settled.
		conversation("k", 8, 15*time.Minute, change("open", "resolved", k, Day/2)),
		// Resolved 7.5 days ago and reopened 4.5 days later: a reopen, not resolved.
		conversation("l", 9, Day, change("open", "resolved", l, 36*time.Hour), change("resolved", "open", l, 6*Day)),
		// Older than every window.
		conversation("m", 40, time.Hour),
	}
	first := ago(40)
	numbers := Compute(Input{Side: Live, Measured: measured, End: measured, HistorySince: ago(60), First: &first, Conversations: conversations,
		Drafts: []Draft{
			{ConversationID: "a", Outcome: "sent_unchanged", DecidedAt: ago(1)},
			{ConversationID: "a", Outcome: "sent_unchanged", DecidedAt: ago(1)},
			{ConversationID: "b", Outcome: "sent_edited", DecidedAt: ago(2)},
			{ConversationID: "c", Outcome: "discarded", DecidedAt: ago(10)},
			{ConversationID: "f", Outcome: "superseded", DecidedAt: ago(3)},
			{ConversationID: "g", Outcome: "sent_unchanged", DecidedAt: ago(40)},
		}})

	month := numbers["30d"]
	// Replies: 300 600 900 1200 1800 2400 3600 5400 7200 14400 86400 s. Median the 6th, p90 the 10th of 11.
	if want := (FirstResponse{MedianSeconds: ptr[int64](2400), P90Seconds: ptr[int64](14400), Answered: 11, Waiting: 1}); !reflect.DeepEqual(month.FirstResponse, want) {
		t.Errorf("30d first response = %+v, want %+v", month.FirstResponse, want)
	}
	// 12 conversations in 30 days: 2.8 a week.
	if month.Volume.Count != 12 || month.Volume.Days != 30 || *month.Volume.PerWeek != 2.8 || !month.Volume.From.Equal(ago(30)) {
		t.Errorf("30d volume = %+v", month.Volume)
	}
	// Created 37 to 7 days ago: f g h i j k l. Resolved after 3600 43200 86400 259200 432000 s.
	if want := (&Resolution{MedianSeconds: ptr[int64](86400), Resolved: 5, Unresolved: 2}); !reflect.DeepEqual(month.Resolution, want) {
		t.Errorf("30d resolution = %+v, want %+v", month.Resolution, want)
	}
	// Resolutions f1 g2 h1 i1 k1 l1 = 7, reopened g and l: 2 of 7.
	if want := (&Reopened{Rate: ptr(0.286), Reopened: 2, Resolutions: 7}); !reflect.DeepEqual(month.Reopened, want) {
		t.Errorf("30d reopened = %+v, want %+v", month.Reopened, want)
	}
	if want := (&AIDrafts{SentUnchanged: 2, SentEdited: 1, Discarded: 1, ConversationsSentUnchanged: 1}); !reflect.DeepEqual(month.AI, want) {
		t.Errorf("30d AI = %+v, want %+v", month.AI, want)
	}
	if month.Handle != nil || month.AvailableOn != nil {
		t.Errorf("30d handle = %+v available = %v, want none for Colliedesk", month.Handle, month.AvailableOn)
	}
	members := Members(numbers)
	for key, want := range map[string][]string{
		"30d.first_response":         {"a", "b", "c", "d", "f", "g", "h", "i", "j", "k", "l"},
		"30d.first_response_waiting": {"e"},
		"30d.volume":                 {"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
		"30d.resolution":             {"f", "g", "h", "i", "k"},
		"30d.unresolved":             {"j", "l"},
		"30d.reopened":               {"g", "l"},
		"30d.ai_sent_unchanged":      {"a"},
		"30d.ai_sent_edited":         {"b"},
		"30d.ai_discarded":           {"c"},
		"7d.resolution":              {"f", "g", "k"},
	} {
		if got := sorted(members[key]); !reflect.DeepEqual(got, want) {
			t.Errorf("members %s = %v, want %v", key, got, want)
		}
	}

	week := numbers["7d"]
	// a b c d answered, e waiting: 4 replies are too few for a median.
	if want := (FirstResponse{Answered: 4, Waiting: 1}); !reflect.DeepEqual(week.FirstResponse, want) {
		t.Errorf("7d first response = %+v, want %+v", week.FirstResponse, want)
	}
	if week.Volume.Count != 5 || *week.Volume.PerWeek != 5 {
		t.Errorf("7d volume = %+v", week.Volume)
	}
	// Created 14 to 7 days ago: f g k resolved, l reopened. Resolutions f1 g2 k1 l1 = 5, 2 reopened.
	if want := (&Resolution{Resolved: 3, Unresolved: 1}); !reflect.DeepEqual(week.Resolution, want) {
		t.Errorf("7d resolution = %+v, want %+v", week.Resolution, want)
	}
	if want := (&Reopened{Rate: ptr(0.4), Reopened: 2, Resolutions: 5}); !reflect.DeepEqual(week.Reopened, want) {
		t.Errorf("7d reopened = %+v, want %+v", week.Reopened, want)
	}
	if want := (&AIDrafts{SentUnchanged: 2, SentEdited: 1, ConversationsSentUnchanged: 1}); !reflect.DeepEqual(week.AI, want) {
		t.Errorf("7d AI = %+v, want %+v", week.AI, want)
	}
}

// A window whose resolutions need status history from before it was kept says when it will be ready.
func TestResolutionWaitsForStatusHistory(t *testing.T) {
	numbers := Compute(Input{Side: Live, Measured: measured, End: measured, HistorySince: ago(20)})
	if want := ago(20).Add(37 * Day); numbers["30d"].AvailableOn == nil || !numbers["30d"].AvailableOn.Equal(want) || numbers["30d"].Resolution != nil || numbers["30d"].Reopened != nil {
		t.Errorf("30d = available %v resolution %+v, want available on %s and no resolution", numbers["30d"].AvailableOn, numbers["30d"].Resolution, want)
	}
	if numbers["7d"].AvailableOn != nil || numbers["7d"].Resolution == nil {
		t.Errorf("7d = available %v resolution %+v, want ready", numbers["7d"].AvailableOn, numbers["7d"].Resolution)
	}
}

// A team that has just moved has no customer conversation in Colliedesk yet: its volume is not a
// rate of zero a week, which would read "100% fewer" next to the baseline.
func TestNoCustomerConversationYetHasNoWeeklyVolume(t *testing.T) {
	numbers := Compute(Input{Side: Live, Measured: measured, End: measured, HistorySince: ago(1)})
	for _, key := range []string{"30d", "7d"} {
		if volume := numbers[key].Volume; volume.Count != 0 || volume.PerWeek != nil || volume.Days != 0 {
			t.Errorf("%s volume = %+v, want no conversations and no weekly rate", key, volume)
		}
	}
	// Once the first one came in long ago, a quiet window is a real zero.
	numbers = Compute(Input{Side: Live, Measured: measured, End: measured, HistorySince: ago(60), First: ptr(ago(60))})
	if volume := numbers["7d"].Volume; volume.PerWeek == nil || *volume.PerWeek != 0 {
		t.Errorf("7d volume after a quiet week = %+v, want 0 a week", volume)
	}
}

func TestBaselineNumbersFollowTheCountingRules(t *testing.T) {
	end := measured.Add(-Day) // the newest imported ticket, a day before the import finished
	at := func(days float64) time.Time { return end.Add(-time.Duration(days * float64(Day))) }
	ticket := func(id string, days float64, reply time.Duration, stats *Stats, handle int) Conversation {
		created := at(days)
		c := Conversation{ID: id, CreatedAt: created, FirstIncoming: created, Stats: stats, HandleMinutes: handle}
		if reply > 0 {
			c.FirstReply = after(created, reply)
		}
		return c
	}
	resolved := func(status string, days float64, d time.Duration) *Stats {
		return &Stats{Status: status, ResolvedAt: after(at(days), d)}
	}
	conversations := []Conversation{
		ticket("p", 2, 10*time.Minute, resolved("resolved", 2, time.Hour), 0),
		ticket("q", 8, 20*time.Minute, resolved("closed", 8, 2*time.Hour), 30),
		ticket("r", 9, 30*time.Minute, resolved("resolved", 9, Day), 10),
		// Open again in Freshdesk when the stats were read: its resolve time does not count.
		ticket("s", 10, 40*time.Minute, resolved("open", 10, time.Hour), 50),
		// Stats never read (an import before they were), and resolved without a resolve time.
		ticket("t", 11, 50*time.Minute, nil, 0),
		ticket("u", 12, time.Hour, &Stats{Status: "resolved"}, 0),
		ticket("v", 13, 70*time.Minute, resolved("resolved", 13, 3*time.Hour), 20),
		ticket("w", 14, 80*time.Minute, resolved("closed", 14, 4*time.Hour), 40),
		ticket("x", 15, 0, resolved("resolved", 15, 5*time.Hour), 60),
		// Resolved 6.5 days before the newest ticket, 7.5 days before the import: settled.
		ticket("y", 16, 100*time.Minute, &Stats{Status: "resolved", ResolvedAt: ptr(at(6.5))}, 0),
	}
	first := at(16)
	numbers := Compute(Input{Side: Baseline, Measured: measured, End: end, First: &first, Conversations: conversations})

	month := numbers["30d"]
	// Replies 600 1200 1800 2400 3000 3600 4200 4800 6000 s: median the 5th, p90 the 9th of 9.
	if want := (FirstResponse{MedianSeconds: ptr[int64](3000), P90Seconds: ptr[int64](6000), Answered: 9, Waiting: 1}); !reflect.DeepEqual(month.FirstResponse, want) {
		t.Errorf("30d first response = %+v, want %+v", month.FirstResponse, want)
	}
	// 10 tickets since the first, 16 days before the end: 4.4 a week.
	if month.Volume.Count != 10 || month.Volume.Days != 16 || *month.Volume.PerWeek != 4.4 || !month.Volume.From.Equal(first) {
		t.Errorf("30d volume = %+v", month.Volume)
	}
	// Created 37 to 7 days before the end: q..y. Resolved after 7200 10800 14400 18000 86400 820800 s:
	// median (14400 + 18000) / 2. s is open, t and u have no resolve time.
	if want := (&Resolution{MedianSeconds: ptr[int64](16200), Resolved: 6, Unresolved: 1, WithoutResolveTime: 2}); !reflect.DeepEqual(month.Resolution, want) {
		t.Errorf("30d resolution = %+v, want %+v", month.Resolution, want)
	}
	// Time logged on the resolved: q 30, r 10, v 20, w 40, x 60 minutes.
	if want := (&Handle{MedianMinutes: ptr[int64](30), Conversations: 5}); !reflect.DeepEqual(month.Handle, want) {
		t.Errorf("30d handle = %+v, want %+v", month.Handle, want)
	}
	if month.Reopened != nil || month.AI != nil || month.AvailableOn != nil {
		t.Errorf("30d reopened = %+v AI = %+v available = %v, want none for the baseline", month.Reopened, month.AI, month.AvailableOn)
	}
	week := numbers["7d"]
	// Created 14 to 7 days before the end: q r s t u v (w is exactly 14 days, outside).
	if want := (&Resolution{Resolved: 3, Unresolved: 1, WithoutResolveTime: 2}); !reflect.DeepEqual(week.Resolution, want) {
		t.Errorf("7d resolution = %+v, want %+v", week.Resolution, want)
	}
	if want := (&Handle{Conversations: 3}); !reflect.DeepEqual(week.Handle, want) {
		t.Errorf("7d handle = %+v, want %+v", week.Handle, want)
	}
	if week.Volume.Count != 1 || *week.Volume.PerWeek != 1 {
		t.Errorf("7d volume = %+v", week.Volume)
	}
}
