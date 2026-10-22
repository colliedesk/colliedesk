package freshdesk_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// recentSince selects tickets 2, 4 and 6 of the fixture (4 is deleted at the source).
var recentSince = time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

func TestRecentModeImportsNewestFirstAndLeavesAccountWideStepsToTheFullPass(t *testing.T) {
	env := newImportEnv(t)
	env.fake.solutions, env.fake.canned = true, true
	var mu sync.Mutex
	var paths []string
	env.fake.onRequest = func(path string) {
		mu.Lock()
		paths = append(paths, path)
		mu.Unlock()
	}
	report, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
		o.Mode, o.Since = freshdesk.ModeRecent, &recentSince
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Interim || report.Status != freshdesk.StatusInterim {
		t.Fatalf("interim=%v status=%s, want an interim report", report.Interim, report.Status)
	}
	if md := report.Markdown(); !strings.Contains(md, "# Freshdesk import: INTERIM") || !strings.Contains(md, "only its report can be OK") {
		t.Fatalf("markdown does not say the report is interim:\n%s", md)
	}
	var fetched []string
	mu.Lock()
	for _, path := range paths {
		rest, ok := strings.CutPrefix(path, "/api/v2/tickets/")
		if ok && !strings.Contains(rest, "/") {
			fetched = append(fetched, rest)
		}
		for _, refused := range []string{"/api/v2/tickets/archived/", "/api/v2/solutions/", "/api/v2/canned_response_folders", "/api/v2/search/"} {
			if strings.HasPrefix(path, refused) {
				t.Errorf("the recent pass called %s; it belongs to the full pass", path)
			}
		}
		if path == "/api/v2/contacts" {
			t.Error("the recent pass listed every contact; it reads requesters on demand")
		}
	}
	mu.Unlock()
	if got := strings.Join(fetched, ","); got != "6,4,2" {
		t.Fatalf("ticket fetch order = %s, want newest updated first: 6,4,2", got)
	}
	if got := env.count(t, "conversations"); got != 2 {
		t.Fatalf("conversations after the recent pass = %d, want 2 (tickets 2 and 6)", got)
	}
	assertCount(t, "tickets", report.Tickets.EntityCount, 2, 2)

	full := env.mustRun(t)
	if full.Status != freshdesk.StatusOK || full.Interim {
		t.Fatalf("full pass status=%s interim=%v missing=%v failures=%+v", full.Status, full.Interim, full.Missing, full.Failures)
	}
	if got := env.tableCounts(t); !strings.HasPrefix(got, "contacts=4 conversations=5 messages=11 attachments=3 time_entries=3") {
		t.Fatalf("rows after the full pass = %s", got)
	}
	for _, id := range []string{"2", "6"} {
		if n := env.fake.count("/api/v2/tickets/" + id); n != 1 {
			t.Errorf("ticket %s was read %d times; the full pass must skip what the recent pass imported", id, n)
		}
	}
}

func TestRecentModeNeedsASinceAndARealRun(t *testing.T) {
	env := newImportEnv(t)
	if _, err := env.run(t, t.Context(), func(o *freshdesk.Options) { o.Mode = freshdesk.ModeRecent }); err == nil {
		t.Fatal("a recent pass without since ran")
	}
	if _, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
		o.Mode, o.Since, o.DryRun = freshdesk.ModeRecent, &recentSince, true
	}); err == nil {
		t.Fatal("a recent dry run ran")
	}
	if n := env.count(t, "conversations"); n != 0 {
		t.Fatalf("conversations = %d, want 0", n)
	}
}

func TestRecentModeOnACappedPlanRefusesASliceThatDoesNotFitBeforeWriting(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "solo")
	before := env.rowCounts(t)
	_, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
		o.Mode, o.Since, o.Cloud, o.PaidStorageBytes = freshdesk.ModeRecent, &recentSince, true, 1
	})
	var capErr *freshdesk.StorageCapError
	if !errors.As(err, &capErr) {
		t.Fatalf("err = %v, want a storage cap error", err)
	}
	if after := env.rowCounts(t); after != before {
		t.Fatalf("rows changed from %s to %s: a refused slice must write nothing", before, after)
	}
	if env.fake.count("/api/v2/tickets/archived/3") != 0 {
		t.Fatal("the recent pass sized the whole account; it sizes only its slice")
	}
}

func TestOnProgressReportsPhasesCountsAndRateLimitWaitsWithoutAWriter(t *testing.T) {
	env := newImportEnv(t)
	env.fake.throttleContacts = true
	var mu sync.Mutex
	var events []freshdesk.ProgressEvent
	report, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
		o.OnProgress = func(e freshdesk.ProgressEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
	})
	if err != nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%v err=%v", report, err)
	}
	mu.Lock()
	defer mu.Unlock()
	var sawTickets, sawWait bool
	calls := 0
	for _, e := range events {
		if e.APICalls < calls {
			t.Fatalf("api calls went down: %+v", events)
		}
		calls = e.APICalls
		if e.Phase == "importing tickets" && e.Total == 6 && e.Done == 6 {
			sawTickets = true
		}
		if e.RateLimitUntil != nil {
			sawWait = true
		}
	}
	if !sawTickets || !sawWait || calls == 0 {
		t.Fatalf("tickets=%v wait=%v calls=%d in %+v", sawTickets, sawWait, calls, events)
	}
}

func TestRateLimitReserve(t *testing.T) {
	for _, reserve := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[reserve], func(t *testing.T) { checkRateLimitReserve(t, reserve) })
	}
}

// checkRateLimitReserve: with the reserve on, the run leaves calls to the account's other integrations.
func checkRateLimitReserve(t *testing.T, reserve bool) {
	env := newImportEnv(t)
	env.fake.remaining = "50" // of 400: below a 20 % reserve of 80
	report, err := env.run(t, t.Context(), func(o *freshdesk.Options) { o.ReserveRateLimit = reserve })
	if err != nil {
		t.Fatal(err)
	}
	paused := 0
	for _, d := range env.sleeps {
		if d == 61*time.Second {
			paused++
		}
	}
	if reserve && (paused == 0 || report.RateLimit.Pauses == 0) {
		t.Fatalf("reserve on: %d pauses, report %+v; want a pause when remaining <= reserve", paused, report.RateLimit)
	}
	if !reserve && paused != 0 {
		t.Fatalf("reserve off: %d pauses; the CLI keeps using the whole limit", paused)
	}
}

func TestPreviewCountsTheAccountWithoutWriting(t *testing.T) {
	env := newImportEnv(t)
	env.fake.canned = true
	preview, err := freshdesk.Preview(t.Context(), env.options())
	if err != nil {
		t.Fatal(err)
	}
	if preview.AccountID != 777 || preview.Source != "freshdesk:account:777" {
		t.Fatalf("account = %d %s", preview.AccountID, preview.Source)
	}
	if len(preview.Agents) != 2 || preview.Agents[0].Email != "alice@acme.example" || !preview.Agents[0].Active || preview.Agents[1].Active {
		t.Fatalf("agents = %+v", preview.Agents)
	}
	if preview.Companies != 2 || preview.Contacts == nil || *preview.Contacts != 3 {
		t.Fatalf("companies=%d contacts=%v", preview.Companies, preview.Contacts)
	}
	if preview.TicketsOutsideArchive == nil || *preview.TicketsOutsideArchive != 5 || preview.HighestListedTicketID != 6 {
		t.Fatalf("tickets=%v highest=%d", preview.TicketsOutsideArchive, preview.HighestListedTicketID)
	}
	if preview.Archive != freshdesk.AccessReadable || preview.CannedResponses != freshdesk.AccessReadable || preview.KnowledgeBase != freshdesk.AccessNone || preview.RateLimitPerMinute != 400 {
		t.Fatalf("access = %+v", preview)
	}
	if got := env.tableCounts(t); got != "contacts=0 conversations=0 messages=0 attachments=0 time_entries=0 tags=0 conversation_tags=0" {
		t.Fatalf("preview wrote rows: %s", got)
	}
	for _, path := range []string{"/api/v2/contacts", "/api/v2/tickets/6", "/api/v2/tickets/6/conversations"} {
		if n := env.fake.count(path); n != 0 {
			t.Errorf("preview called %s %d times", path, n)
		}
	}
}

func TestPreviewSaysWhatTheKeyCannotRead(t *testing.T) {
	env := newImportEnv(t)
	env.fake.archiveForbidden, env.fake.failSearch = true, true
	env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders": 403}
	preview, err := freshdesk.Preview(t.Context(), env.options())
	if err != nil {
		t.Fatal(err)
	}
	if preview.Archive != freshdesk.AccessRefused || preview.Contacts != nil || preview.TicketsOutsideArchive != nil || preview.CannedResponses != freshdesk.AccessRefused {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestPreviewWithAKeyThatIsNotAnAdminsSaysSo(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failPathStatus = map[string]int{"/api/v2/agents": 403}
	if _, err := freshdesk.Preview(t.Context(), env.options()); !errors.Is(err, freshdesk.ErrNeedsAdminKey) {
		t.Fatalf("err = %v, want ErrNeedsAdminKey", err)
	}
}

func TestPreviewWithAWrongKeyIsRejectedAndNeverEchoesIt(t *testing.T) {
	env := newImportEnv(t)
	options := env.options()
	options.APIKey = "wrong-key-canary"
	_, err := freshdesk.Preview(context.Background(), options)
	if !errors.Is(err, freshdesk.ErrKeyRejected) {
		t.Fatalf("err = %v, want ErrKeyRejected", err)
	}
	if strings.Contains(err.Error(), "wrong-key-canary") || !strings.Contains(err.Error(), "Freshdesk rejected the API key") {
		t.Fatalf("err = %q", err)
	}
	if _, err := env.run(t, t.Context(), func(o *freshdesk.Options) { o.APIKey = "wrong-key-canary" }); !errors.Is(err, freshdesk.ErrKeyRejected) {
		t.Fatalf("run err = %v, want ErrKeyRejected", err)
	}
}

func TestMessagesKeepTheFreshdeskAgentIDForRelinking(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	rows, err := env.pool.Query(context.Background(), `SELECT origin->>'external_id', origin->>'author_external_id' FROM messages
		WHERE workspace_id=$1 AND origin->>'kind'<>'description' ORDER BY 1`, env.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id string
		var author *string
		if err := rows.Scan(&id, &author); err != nil {
			t.Fatal(err)
		}
		got[id] = "-"
		if author != nil {
			got[id] = *author
		}
	}
	// 7001 is the customer, 7002 and 7006 are Alice (matched), 7003 and 7004 Bob (not matched).
	want := map[string]string{"7001": "-", "7002": "101", "7003": "102", "7004": "102", "7006": "101"}
	for id, author := range want {
		if got[id] != author {
			t.Errorf("message %s author_external_id = %q, want %q (all: %v)", id, got[id], author, got)
		}
	}
	var responder *string
	if err := env.pool.QueryRow(context.Background(), `SELECT origin->>'responder_external_id' FROM conversations
		WHERE workspace_id=$1 AND origin->>'external_id'='2'`, env.workspaceID).Scan(&responder); err != nil || responder == nil || *responder != "102" {
		t.Fatalf("ticket 2 responder_external_id = %v (%v), want 102", responder, err)
	}
}

func TestAPlanWithoutAPICallsIsNamedNotRetried(t *testing.T) {
	env := newImportEnv(t)
	env.fake.noAPI = true
	if _, err := freshdesk.Preview(t.Context(), env.options()); !errors.Is(err, freshdesk.ErrPlanNoAPI) {
		t.Fatalf("preview err = %v, want ErrPlanNoAPI", err)
	}
	if _, err := env.run(t, t.Context(), nil); !errors.Is(err, freshdesk.ErrPlanNoAPI) {
		t.Fatalf("run err = %v, want ErrPlanNoAPI", err)
	}
	if len(env.sleeps) != 0 {
		t.Fatalf("waited %v for a limit that never comes back", env.sleeps)
	}
}

func TestAKeyRevokedMidRunStopsTheRunAndKeepsWhatWasImported(t *testing.T) {
	for _, mode := range []freshdesk.Mode{freshdesk.ModeFull, freshdesk.ModeRecent} {
		t.Run(map[freshdesk.Mode]string{freshdesk.ModeFull: "full", freshdesk.ModeRecent: "recent"}[mode], func(t *testing.T) {
			env := newImportEnv(t)
			env.fake.revokeKeyAfter = "/api/v2/tickets/6/conversations"
			_, err := env.run(t, t.Context(), func(o *freshdesk.Options) {
				if mode == freshdesk.ModeRecent {
					o.Mode, o.Since = mode, &recentSince
				}
			})
			if !errors.Is(err, freshdesk.ErrKeyRejected) {
				t.Fatalf("err = %v, want ErrKeyRejected", err)
			}
			if n := env.fake.count("/api/v2/tickets/2"); n != 0 && mode == freshdesk.ModeRecent {
				t.Fatalf("the run went on to ticket 2 (%d reads) after the key was refused", n)
			}
		})
	}
}

func TestAnAccountThatKeepsFailingIsUnavailableNotRefused(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failPathTimes = map[string]int{"/api/v2/agents": 100}
	_, err := env.run(t, t.Context(), nil)
	if !errors.Is(err, freshdesk.ErrUnavailable) || errors.Is(err, freshdesk.ErrKeyRejected) {
		t.Fatalf("err = %v, want ErrUnavailable: the job retries it later", err)
	}
	env.fake.failPathTimes = nil
	env.fake.failPathStatus = map[string]int{"/api/v2/agents": 400}
	if _, err := env.run(t, t.Context(), nil); errors.Is(err, freshdesk.ErrUnavailable) {
		t.Fatalf("a 400 is not worth retrying: %v", err)
	}
}
