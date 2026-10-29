package freshdesk_test

import (
	"testing"
)

func perTicketTimeEntryRequests(env *importEnv) int {
	total := 0
	for _, id := range []string{"1", "2", "6"} {
		total += env.fake.count("/api/v2/tickets/" + id + "/time_entries")
	}
	return total
}

func TestTimeEntriesAreListedOnceForTheAccountNotOncePerTicket(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)

	assertCount(t, "time entries", report.TimeEntries, 3, 3)
	if got := env.tableCounts(t); got != fullImportRows {
		t.Fatalf("rows = %s", got)
	}
	if got := perTicketTimeEntryRequests(env); got != 0 {
		t.Fatalf("per-ticket time entry requests = %d, want 0: the account's entries are listed in one paged call", got)
	}
	if got := env.fake.count("/api/v2/time_entries"); got == 0 || got > 2 {
		t.Fatalf("time entries list requests = %d, want 1 or 2 pages", got)
	}
	t.Logf("Freshdesk API requests for the full fixture import: %d", env.fake.apiCalls())
}

func TestTimeEntriesFallBackToPerTicketWhenTheAccountListIsRefused(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failPathStatus = map[string]int{"/api/v2/time_entries": 403}
	report := env.mustRun(t)

	assertCount(t, "time entries", report.TimeEntries, 3, 3)
	if got := env.tableCounts(t); got != fullImportRows {
		t.Fatalf("rows = %s", got)
	}
	if got := perTicketTimeEntryRequests(env); got == 0 {
		t.Fatal("the importer gave up on time entries instead of asking ticket by ticket")
	}
}
