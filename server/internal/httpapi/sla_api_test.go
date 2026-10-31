package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

type slaClockBody struct {
	State               string     `json:"state"`
	DueAt               *time.Time `json:"due_at"`
	WarnAt              *time.Time `json:"warn_at"`
	MetAt               *time.Time `json:"met_at"`
	BreachedAt          *time.Time `json:"breached_at"`
	DueSoon             bool       `json:"due_soon"`
	Overdue             bool       `json:"overdue"`
	BusinessMinutesLeft *int       `json:"business_minutes_left"`
}

type slaConversation struct {
	ID  string `json:"id"`
	SLA *struct {
		State         string        `json:"state"`
		FirstResponse *slaClockBody `json:"first_response"`
		Resolution    *slaClockBody `json:"resolution"`
	} `json:"sla"`
}

// Every hour open, so a target of an hour is due an hour after the conversation opens (domain tests hold
// the calendar itself).
func roundTheClock() map[string]any {
	hours := []map[string]any{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		hours = append(hours, map[string]any{"day": day, "open": true, "from": "00:00", "to": "23:59"})
	}
	hour := 60
	return map[string]any{"enabled": true, "timezone": "UTC", "hours": hours, "warn_before_minutes": 10, "alert_email": true,
		"targets": map[string]any{"urgent": map[string]any{"first_response_minutes": hour, "resolution_minutes": 2 * hour},
			"high": map[string]any{"first_response_minutes": hour}, "normal": map[string]any{"first_response_minutes": hour}, "low": map[string]any{"first_response_minutes": hour}}}
}

// A conversation opened while the targets are on carries its clocks: due an hour after it opens, a
// resolution clock once it is urgent, paused while pending, met by the first reply. Overdue and due-soon
// conversations are filtered and sorted by the time they are due; off, no conversation has targets, and an
// imported one never has.
func TestAConversationCarriesItsResponseClocks(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Clocks Desk", "admin@clocks.example.com")
	other := createSession(t, api, service, "Clocks Other", "admin@clocks-other.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@clocks.example.com"})
	before := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Opened before the targets"})
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", roundTheClock(), admin, nil); response.Code != http.StatusOK {
		t.Fatalf("targets: %d %s", response.Code, response.Body.String())
	}
	calendar, err := domain.NewBusinessCalendar("UTC", []domain.BusinessDay{{Day: "mon", Open: true, From: "00:00", To: "23:59"}, {Day: "tue", Open: true, From: "00:00", To: "23:59"},
		{Day: "wed", Open: true, From: "00:00", To: "23:59"}, {Day: "thu", Open: true, From: "00:00", To: "23:59"}, {Day: "fri", Open: true, From: "00:00", To: "23:59"},
		{Day: "sat", Open: true, From: "00:00", To: "23:59"}, {Day: "sun", Open: true, From: "00:00", To: "23:59"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	get := func(token, id string) slaConversation {
		t.Helper()
		var item slaConversation
		response := request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", id, response.Code)
		}
		decode(t, response, &item)
		return item
	}
	patch := func(id string, body map[string]any) {
		t.Helper()
		if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+id, body, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("PATCH %v: %d %s", body, response.Code, response.Body.String())
		}
	}
	if got := get(admin, before); got.SLA != nil {
		t.Fatalf("one opened before the targets has them: %+v", got.SLA)
	}
	id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})
	created, err := time.Parse(time.RFC3339Nano, queryString(t, pool, `SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM conversations WHERE id=$1`, id))
	if err != nil {
		t.Fatal(err)
	}
	item := get(admin, id)
	if item.SLA == nil || item.SLA.FirstResponse == nil || item.SLA.FirstResponse.State != "running" || item.SLA.Resolution != nil ||
		!item.SLA.FirstResponse.DueAt.Equal(calendar.Add(created, time.Hour)) || item.SLA.State != "ok" {
		t.Fatalf("opened = %+v", item.SLA)
	}
	patch(id, map[string]any{"priority": "urgent"})
	if item := get(admin, id); item.SLA.Resolution == nil || item.SLA.Resolution.State != "running" || !item.SLA.Resolution.DueAt.Equal(calendar.Add(created, 2*time.Hour)) {
		t.Fatalf("urgent = %+v", item.SLA)
	}
	patch(id, map[string]any{"status": "pending"})
	if item := get(admin, id); item.SLA.FirstResponse.State != "paused" || item.SLA.Resolution.State != "paused" || item.SLA.State != "paused" {
		t.Fatalf("pending = %+v", item.SLA)
	}
	patch(id, map[string]any{"status": "open"})
	if item := get(admin, id); item.SLA.FirstResponse.State != "running" {
		t.Fatalf("open again = %+v", item.SLA)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+id+"/messages", map[string]any{"kind": "note", "body": "Looking"}, admin, nil); response.Code != http.StatusCreated {
		t.Fatalf("note: %d", response.Code)
	}
	if item := get(admin, id); item.SLA.FirstResponse.State != "running" {
		t.Fatalf("a note met the first response: %+v", item.SLA.FirstResponse)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+id+"/messages", map[string]any{"kind": "outgoing", "body": "On it."}, admin, nil); response.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s", response.Code, response.Body.String())
	}
	if item := get(admin, id); item.SLA.FirstResponse.State != "met" || item.SLA.FirstResponse.MetAt == nil {
		t.Fatalf("after the reply = %+v", item.SLA.FirstResponse)
	}
	// Late and nearly late: opened three hours and 55 minutes ago.
	aged := func(subject string, ago time.Duration) string {
		t.Helper()
		id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": subject})
		if _, err := pool.Exec(context.Background(), `UPDATE conversations SET created_at=now()-make_interval(secs => $2),sla_stale=true WHERE id=$1`, id, ago.Seconds()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	late, soon := aged("Late", 3*time.Hour), aged("Soon", 55*time.Minute)
	if item := get(admin, late); !item.SLA.FirstResponse.Overdue || item.SLA.FirstResponse.BreachedAt == nil || item.SLA.State != "overdue" ||
		item.SLA.FirstResponse.BusinessMinutesLeft == nil || *item.SLA.FirstResponse.BusinessMinutesLeft > -110 {
		t.Fatalf("late = %+v", item.SLA.FirstResponse)
	}
	if item := get(admin, soon); !item.SLA.FirstResponse.DueSoon || item.SLA.FirstResponse.Overdue || item.SLA.State != "due_soon" {
		t.Fatalf("soon = %+v", item.SLA.FirstResponse)
	}
	list := func(token, query string) []string {
		t.Helper()
		var page struct {
			Items []slaConversation `json:"items"`
		}
		response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("?%s: %d %s", query, response.Code, response.Body.String())
		}
		decode(t, response, &page)
		var ids []string
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	if got := list(admin, "sla=overdue"); len(got) != 1 || got[0] != late {
		t.Fatalf("overdue = %v", got)
	}
	if got := list(admin, "sla=due_soon"); len(got) != 1 || got[0] != soon {
		t.Fatalf("due soon = %v", got)
	}
	if got := list(admin, "sla=overdue,due_soon&sort=due"); strings.Join(got, ",") != late+","+soon {
		t.Fatalf("both, due first = %v", got)
	}
	if got := list(admin, "sort=due"); len(got) < 3 || got[0] != late || got[1] != soon {
		t.Fatalf("due first = %v", got)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversations?sla=late", nil, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("?sla=late: %d", response.Code)
	}
	if got := list(other, "sla=overdue,due_soon"); len(got) != 0 {
		t.Fatalf("another workspace's = %v", got)
	}
	// Imported: never a target.
	if _, err := pool.Exec(context.Background(), `INSERT INTO conversations(workspace_id,contact_id,subject,channel) SELECT workspace_id,id,'Imported','imported' FROM contacts WHERE id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	if got := queryString(t, pool, `SELECT sla_applies::text FROM conversations WHERE subject='Imported'`); got != "false" {
		t.Fatalf("imported applies = %s", got)
	}
	// Off: no conversation shows them, nor is filtered by them.
	off := roundTheClock()
	off["enabled"] = false
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", off, admin, nil); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if item := get(admin, late); item.SLA != nil {
		t.Fatalf("off, still = %+v", item.SLA)
	}
	if got := list(admin, "sla=overdue"); len(got) != 0 {
		t.Fatalf("off, overdue = %v", got)
	}
}

// Targets counted in the workspace's timezone count again when it changes.
func TestTheClocksFollowTheWorkspaceTimezone(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Zoned Desk", "admin@zoned.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@zoned.example.com"})
	targets := roundTheClock()
	targets["timezone"] = nil
	hours := []map[string]any{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		hours = append(hours, map[string]any{"day": day, "open": true, "from": "08:00", "to": "17:00"})
	}
	targets["hours"] = hours
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", targets, admin, nil); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Zoned"})
	due := func() time.Time {
		t.Helper()
		var item slaConversation
		decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, admin, nil), &item)
		if item.SLA == nil || item.SLA.FirstResponse == nil || item.SLA.FirstResponse.DueAt == nil {
			t.Fatalf("clock = %+v", item.SLA)
		}
		return *item.SLA.FirstResponse.DueAt
	}
	before := due()
	if response := request(t, api, http.MethodPatch, "/api/v1/workspace/timezone", map[string]any{"timezone": "Asia/Tokyo"}, admin, nil); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if after := due(); after.Equal(before) {
		t.Fatalf("due %s stayed after the workspace moved nine hours", after)
	}
}
