package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// What the reply targets suggestion reads (docs/user/setup-guide.md): the first real reply of imported
// conversations, and only for someone who can still use it.

// historyFixture writes conversations into one workspace, with the contact they are from.
type historyFixture struct {
	t                  *testing.T
	pool               *pgxpool.Pool
	workspace, contact string
}

func newHistoryFixture(t *testing.T, pool *pgxpool.Pool, api http.Handler, token string) historyFixture {
	t.Helper()
	insertImport(t, pool, workspaceSlug(t, api, token), "finished")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "History Person", "email": "history@customer.example"})
	return historyFixture{t: t, pool: pool, workspace: workspaceID(t, api, token), contact: contact}
}

// conversation writes one conversation opened a day ago, imported or native, with an outgoing message after
// each of the given minutes; a notice is an outgoing message the channel sent by itself.
func (f historyFixture) conversation(imported bool, notice *int, replies ...int) {
	f.t.Helper()
	opened := time.Now().UTC().Add(-24 * time.Hour)
	origin, channel := any(nil), "email"
	if imported {
		origin, channel = `{"source":"freshdesk"}`, "imported"
	}
	conversation := queryString(f.t, f.pool, `INSERT INTO conversations(workspace_id,contact_id,subject,status,origin,channel,created_at,updated_at)
		VALUES($1,$2,'History','resolved',$3,$4,$5,$5) RETURNING id::text`, f.workspace, f.contact, origin, channel, opened)
	write := func(kind string, at time.Time, noticeKind any) {
		if _, err := f.pool.Exec(f.t.Context(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,notice_kind,created_at) VALUES($1,$2,$3,'Text',$4,$5)`,
			f.workspace, conversation, kind, noticeKind, at); err != nil {
			f.t.Fatal(err)
		}
	}
	write("incoming", opened, nil)
	if notice != nil {
		write("outgoing", opened.Add(time.Duration(*notice)*time.Minute), "received")
	}
	for _, minutes := range replies {
		write("outgoing", opened.Add(time.Duration(minutes)*time.Minute), nil)
	}
}

func (f historyFixture) clear() {
	f.t.Helper()
	if _, err := f.pool.Exec(f.t.Context(), `DELETE FROM conversations WHERE workspace_id=$1`, f.workspace); err != nil {
		f.t.Fatal(err)
	}
}

func suggestionOf(t *testing.T, api http.Handler, token string) replyTargetsSuggestionView {
	t.Helper()
	suggestion := readSetupTargets(t, api, token).Suggestion
	if suggestion == nil {
		t.Fatal("an admin with reply targets off gets a suggestion")
	}
	return *suggestion
}

func TestSetupMedianFirstReplyLeavesOutNativeConversationsAndAutomaticNotices(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Median Desk", "admin@median.example.com")
	history := newHistoryFixture(t, pool, api, admin)
	notice := 2
	// Imported: answered after 300 minutes, each with a received notice two minutes in that is no reply.
	for range 3 {
		history.conversation(true, &notice, 300)
	}
	// Native conversations answered in 5 minutes are the team's work in Colliedesk, not the imported history.
	for range 5 {
		history.conversation(false, nil, 5)
	}
	got := suggestionOf(t, api, admin)
	if got.Basis != "history" || got.MedianFirstReplyMinutes == nil || *got.MedianFirstReplyMinutes != 300 || got.Preset != "same_day" {
		t.Fatalf("suggestion = %s %s median %s, want same_day history 300", got.Preset, got.Basis, minutesText(got.MedianFirstReplyMinutes))
	}
	// Only notices and native replies: nothing to learn from.
	history.clear()
	history.conversation(true, &notice)
	history.conversation(false, nil, 5)
	if got := suggestionOf(t, api, admin); got.Basis != "no_history" || got.MedianFirstReplyMinutes != nil {
		t.Fatalf("suggestion = %s %s median %s, want same_day no_history null", got.Preset, got.Basis, minutesText(got.MedianFirstReplyMinutes))
	}
}

func TestSetupSuggestionThresholdsAreAnHourAndEightHoursInclusive(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Threshold Desk", "admin@threshold.example.com")
	history := newHistoryFixture(t, pool, api, admin)
	for _, test := range []struct {
		minutes int
		preset  string
	}{{60, "within_the_hour"}, {61, "same_day"}, {480, "same_day"}, {481, "next_business_day"}} {
		history.clear()
		history.conversation(true, nil, test.minutes)
		if got := suggestionOf(t, api, admin); got.Preset != test.preset || got.MedianFirstReplyMinutes == nil || *got.MedianFirstReplyMinutes != test.minutes {
			t.Fatalf("median %d: suggestion = %s median %s, want %s", test.minutes, got.Preset, minutesText(got.MedianFirstReplyMinutes), test.preset)
		}
	}
}

func TestSetupSuggestionIsLeftOutWhenTargetsAreOnOrTheCallerCannotChangeThem(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Left Out Desk", "admin@left-out.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "agent@left-out.example.com", "agent")
	agent := redeemLabelled(t, api, inviteURL, "Agent")
	history := newHistoryFixture(t, pool, api, admin)
	history.conversation(true, nil, 30)
	if got := suggestionOf(t, api, admin); got.Preset != "within_the_hour" {
		t.Fatalf("admin suggestion = %+v", got)
	}
	// An agent may not change the targets (workspace:update): nothing would use the median.
	if got := readSetupTargets(t, api, agent).Suggestion; got != nil {
		t.Fatalf("agent suggestion = %+v, want null", *got)
	}
	body := map[string]any{"enabled": true, "hours": []any{map[string]any{"day": "mon", "open": true, "from": "09:00", "to": "18:00"}},
		"targets": map[string]any{"normal": map[string]any{"first_response_minutes": 480}}}
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", body, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT targets status=%d body=%s", response.Code, response.Body.String())
	}
	if got := readSetupTargets(t, api, admin).Suggestion; got != nil {
		t.Fatalf("suggestion with targets on = %+v, want null", *got)
	}
}

func TestSetupPresetsCountADayInTheHoursTheyApplyWith(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Long Day Desk", "admin@long-day.example.com")
	other := createSession(t, api, service, "Usual Day Desk", "admin@usual-day.example.com")
	sameDay := func(view setupTargetsView) replyTargetPresetView {
		t.Helper()
		for _, preset := range view.Presets {
			if preset.Key == "same_day" {
				return preset
			}
		}
		t.Fatalf("no same_day preset: %+v", view.Presets)
		return replyTargetPresetView{}
	}

	// No hours yet: a preset applies with Monday to Friday, 09:00 to 18:00, and a day is 540 minutes.
	fresh := readSetupTargets(t, api, admin)
	if fresh.Hours.Kept || fresh.Hours.DayMinutes != 540 || len(fresh.Hours.Hours) != 7 || !fresh.Hours.Hours[0].Open || fresh.Hours.Hours[0].From != "09:00" ||
		fresh.Hours.Hours[0].To != "18:00" || fresh.Hours.Hours[5].Open {
		t.Fatalf("hours = %+v, want Monday to Friday 09:00 to 18:00, 540 a day", fresh.Hours)
	}
	if got := sameDay(fresh).Targets["normal"].Resolution; got != 1620 {
		t.Fatalf("same_day normal resolution = %d, want 1620 (3 days of 540)", got)
	}

	// Hours set in Settings (targets still off) are kept: Monday to Friday 08:00 to 20:00 and a short Saturday.
	open := func(day, from, to string) map[string]any {
		return map[string]any{"day": day, "open": true, "from": from, "to": to}
	}
	body := map[string]any{"enabled": false, "targets": map[string]any{},
		"hours": []any{open("mon", "08:00", "20:00"), open("tue", "08:00", "20:00"), open("wed", "08:00", "20:00"), open("thu", "08:00", "20:00"),
			open("fri", "08:00", "20:00"), open("sat", "10:00", "14:00"), map[string]any{"day": "sun", "open": false}}}
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", body, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT targets status=%d body=%s", response.Code, response.Body.String())
	}
	kept := readSetupTargets(t, api, admin)
	if !kept.Hours.Kept || kept.Hours.DayMinutes != 720 || kept.Hours.Hours[5].From != "10:00" || kept.Hours.Hours[0].To != "20:00" {
		t.Fatalf("hours = %+v, want the workspace's own, 720 a day", kept.Hours)
	}
	// 1 day = 720 minutes: Same day's Normal resolves in 3 days and its Low replies in 1.
	if got := sameDay(kept).Targets["normal"]; got.FirstResponse != 480 || got.Resolution != 2160 {
		t.Fatalf("same_day normal = %d/%d, want 480/2160", got.FirstResponse, got.Resolution)
	}
	if got := sameDay(kept).Targets["low"]; got.FirstResponse != 720 || got.Resolution != 3600 {
		t.Fatalf("same_day low = %d/%d, want 720/3600", got.FirstResponse, got.Resolution)
	}
	// Another workspace's hours never count.
	if got := readSetupTargets(t, api, other).Hours; got.Kept || got.DayMinutes != 540 {
		t.Fatalf("other workspace hours = %+v, want the default", got)
	}
}
