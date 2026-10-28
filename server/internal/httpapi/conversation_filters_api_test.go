package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// filterDesk is a workspace with one conversation per trait the inbox filters look at.
type filterDesk struct {
	token, workspace, agent, teammate string
	ada, bob                          string // contacts
	billing, vip                      string // tags
	// conversations by name
	ids map[string]string
}

func seedFilterDesk(t *testing.T, api http.Handler, token string, pool *pgxpool.Pool, prefix string) filterDesk {
	t.Helper()
	ctx := context.Background()
	desk := filterDesk{token: token, ids: map[string]string{}}
	desk.workspace = workspaceID(t, api, token)
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	desk.agent = me.Agent.ID
	if err := pool.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,$2,'agent') RETURNING id::text`,
		desk.workspace, prefix+"-teammate@filters.example.com").Scan(&desk.teammate); err != nil {
		t.Fatal(err)
	}
	desk.ada = create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada Lovelace", "email": prefix + "-ada@analytical.example"})
	desk.bob = create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Bob Builder", "email": prefix + "-bob@builders.example"})
	if _, err := pool.Exec(ctx, `UPDATE contacts SET organization='Analytical Engines Ltd' WHERE id=$1`, desk.ada); err != nil {
		t.Fatal(err)
	}
	desk.billing = create(t, api, token, "/api/v1/tags", map[string]any{"name": "billing"})
	desk.vip = create(t, api, token, "/api/v1/tags", map[string]any{"name": "vip"})
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	for index, row := range []struct {
		name, contact, status, priority, assignee, channel, origin string
		tags                                                       []string
	}{
		{"mail", desk.ada, "open", "urgent", desk.agent, "email", "", []string{desk.billing, desk.vip}},
		{"form", desk.bob, "pending", "high", desk.teammate, "web_form", "", []string{desk.billing}},
		{"imported", desk.ada, "closed", "low", "", "imported", `{"system":"freshdesk","external_id":"1"}`, nil},
		{"started", desk.bob, "resolved", "normal", "", "agent", "", []string{desk.vip}},
		{"report", desk.bob, "open", "normal", desk.agent, "problem_report", "", nil},
	} {
		id := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": row.contact, "subject": prefix + " " + row.name})
		desk.ids[row.name] = id
		created := base.Add(time.Duration(index) * 24 * time.Hour)
		// updated_at runs the other way, so the created and updated orders differ.
		updated := base.Add(time.Duration(10-index) * 24 * time.Hour)
		if _, err := pool.Exec(ctx, `UPDATE conversations SET status=$2,priority=$3,assignee_id=NULLIF($4,'')::uuid,channel=$5,origin=NULLIF($6,'')::jsonb,created_at=$7,updated_at=$8 WHERE id=$1`,
			id, row.status, row.priority, row.assignee, row.channel, row.origin, created, updated); err != nil {
			t.Fatal(err)
		}
		for _, tag := range row.tags {
			if _, err := pool.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id) VALUES($1,$2,$3)`, desk.workspace, id, tag); err != nil {
				t.Fatal(err)
			}
		}
	}
	message := func(name, kind string, at time.Time) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,$3,$4,$5) RETURNING id::text`,
			desk.workspace, desk.ids[name], kind, prefix+" body of "+name, at).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// mail: the customer wrote last (a note after it does not count) and waits since day 3.
	message("mail", "outgoing", base.Add(48*time.Hour))
	message("mail", "incoming", base.Add(72*time.Hour))
	message("mail", "note", base.Add(96*time.Hour))
	// form: waits since day 1, longer than mail.
	message("form", "incoming", base.Add(24*time.Hour))
	// started: the team answered last; its reply could not be delivered.
	message("started", "incoming", base.Add(24*time.Hour))
	failed := message("started", "outgoing", base.Add(48*time.Hour))
	// The desk's replies went out by email; its email channel was removed since.
	if _, err := pool.Exec(ctx, `INSERT INTO channels(workspace_id,type,name,enabled,disabled_at,deleted_at) VALUES($1,'email','Old mail',false,now(),now())`, desk.workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,message_id,channel_type,channel_id,status,attempts,last_error)
		VALUES($1,$2,'email',(SELECT id FROM channels WHERE workspace_id=$1 AND type='email' LIMIT 1),'failed',5,'mailbox full')`, desk.workspace, failed); err != nil {
		t.Fatal(err)
	}
	// report has a reply that went out.
	sent := message("report", "outgoing", base.Add(24*time.Hour))
	if _, err := pool.Exec(ctx, `INSERT INTO channel_outbox(workspace_id,message_id,channel_type,channel_id,status,sent_at) VALUES($1,$2,'email',(SELECT id FROM channels WHERE workspace_id=$1 AND type='email' LIMIT 1),'sent',now())`, desk.workspace, sent); err != nil {
		t.Fatal(err)
	}
	// form has a reply draft waiting; imported has one that expired before anyone looked.
	for _, draft := range []struct {
		name    string
		expires string
	}{{"form", "1 day"}, {"imported", "-1 minute"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO agent_requests(workspace_id,token_label,token_scope,kind,method,route,path,summary,conversation_id,expires_at)
			VALUES($1,'Claude','read','reply_draft','POST','POST /api/v1/conversations/{id}/messages','/api/v1/conversations/'||$2::text||'/messages','Reply',$2::uuid,now()+$3::interval)`,
			desk.workspace, desk.ids[draft.name], draft.expires); err != nil {
			t.Fatal(err)
		}
	}
	return desk
}

func (desk filterDesk) list(t *testing.T, api http.Handler, query string) []string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, desk.token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET ?%s = %d %s", query, response.Code, response.Body.String())
	}
	ids := conversationIDs(t, response)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := "?" + id
		for key, value := range desk.ids {
			if value == id {
				name = key
			}
		}
		names = append(names, name)
	}
	return names
}

func TestConversationFiltersNarrowTheInbox(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "Filter Desk", "admin@filters.example.com")
	desk := seedFilterDesk(t, api, token, pool, "a")
	inOrder := func(query string, want ...string) {
		t.Helper()
		if got := desk.list(t, api, query); !slices.Equal(got, want) {
			t.Fatalf("?%s = %v, want %v", query, got, want)
		}
	}
	sorted := func(query string, want ...string) {
		t.Helper()
		got := desk.list(t, api, query)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("?%s = %v, want %v", query, got, want)
		}
	}
	// Default order: most recently updated first.
	inOrder("", "mail", "form", "imported", "started", "report")
	// The single values of the first inbox keep working.
	// An empty value is no filter, as it always was.
	inOrder("status=&assignee=&tag=&search=", "mail", "form", "imported", "started", "report")
	sorted("status=open", "mail", "report")
	sorted("assignee=me", "mail", "report")
	sorted("assignee=unassigned", "imported", "started")
	sorted("assignee=anyone", "mail", "form", "report")
	sorted("tag="+desk.vip, "mail", "started")

	sorted("status=open,pending", "mail", "form", "report")
	sorted("priority=high,urgent", "mail", "form")
	sorted("assignee="+desk.teammate, "form")
	sorted("assignee=unassigned,"+desk.teammate, "form", "imported", "started")
	sorted("tag="+desk.billing+","+desk.vip, "mail", "form", "started")
	sorted("tag="+desk.billing+","+desk.vip+"&tag_match=all", "mail")
	sorted("contact="+desk.ada, "mail", "imported")
	sorted("organization="+url.QueryEscape("analytical engines ltd"), "mail", "imported")
	sorted("awaiting=team", "mail", "form")
	sorted("awaiting=customer", "started", "report")
	sorted("has_pending_draft=true", "form")
	sorted("has_pending_draft=false", "mail", "imported", "started", "report")
	sorted("delivery_failed=true", "started")
	sorted("delivery_failed=false", "mail", "form", "imported", "report")
	sorted("source=imported", "imported")
	sorted("source=native", "mail", "form", "started", "report")
	sorted("channel=email,web_form", "mail", "form")
	sorted("channel=problem_report", "report")
	// After includes the instant, before excludes it.
	sorted("created_after=2026-09-02T08:00:00Z&created_before=2026-09-04T08:00:00Z", "form", "imported")
	// 10:00:01 at +02:00 is one second after imported's 08:00 UTC.
	sorted("updated_after="+url.QueryEscape("2026-09-09T10:00:01+02:00"), "mail", "form")
	sorted("updated_after=2026-09-09T08:00:00Z", "mail", "form", "imported")
	sorted("updated_before=2026-09-08T08:00:00Z", "report")
	// Search reaches the subject, the messages and the contact's name and email.
	sorted("search=lovelace", "mail", "imported")
	sorted("search=builders.example", "form", "started", "report")
	sorted("search="+url.QueryEscape("body of form"), "form")
	// Filters combine.
	sorted("status=open,pending&awaiting=team&priority=urgent", "mail")

	inOrder("sort=updated_asc", "report", "started", "imported", "form", "mail")
	inOrder("sort=created_desc", "report", "started", "imported", "form", "mail")
	inOrder("sort=created_asc", "mail", "form", "imported", "started", "report")
	// Urgent first; the two normal ones by last update.
	inOrder("sort=priority", "mail", "form", "started", "report", "imported")
	// The customer who waits longest first, then everything else by last update.
	inOrder("sort=waiting_longest", "form", "mail", "imported", "started", "report")
}

func TestAConversationATeamMemberStartsHasTheAgentChannel(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Channel Desk", "admin@channel.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@channel.example.com"})
	id := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Following up"})
	var got struct {
		Channel string `json:"channel"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &got)
	if got.Channel != "agent" {
		t.Fatalf("channel=%q, want agent", got.Channel)
	}
}

func TestConversationListPagesAreStableWhenSortKeysTie(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "Tie Desk", "admin@ties.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Tie", "email": "tie@ties.example.com"})
	for index := range 7 {
		create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": fmt.Sprint("Tie ", index)})
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET created_at='2026-09-01T08:00:00Z',updated_at='2026-09-01T08:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	for _, sort := range []string{"updated_desc", "updated_asc", "created_desc", "created_asc", "priority", "waiting_longest"} {
		var all []string
		for page := 1; page <= 4; page++ {
			all = append(all, conversationIDs(t, request(t, api, http.MethodGet, fmt.Sprintf("/api/v1/conversations?sort=%s&page_size=2&page=%d", sort, page), nil, token, nil))...)
		}
		seen := map[string]bool{}
		for _, id := range all {
			if seen[id] {
				t.Fatalf("sort=%s repeats %s across pages: %v", sort, id, all)
			}
			seen[id] = true
		}
		if len(all) != 7 {
			t.Fatalf("sort=%s pages hold %d rows, want 7", sort, len(all))
		}
	}
}

func TestConversationFiltersRefuseValuesTheyDoNotKnow(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Strict Desk", "admin@strict.example.com")
	for _, query := range []string{
		"status=opened", "status=open,", "priority=critical", "assignee=someone", "tag=not-a-uuid", "tag_match=some",
		"contact=42", "awaiting=nobody", "has_pending_draft=yes", "delivery_failed=1", "source=freshdesk",
		"channel=phone", "created_after=yesterday", "updated_before=2026-09-01", "sort=random",
		"organization=" + strings.Repeat("x", 201),
	} {
		response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("?%s = %d, want 400: %s", query, response.Code, response.Body.String())
		}
	}
}

// An id from another workspace never matches here, and nothing of that workspace is listed.
func TestConversationFiltersNeverCrossWorkspaces(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Filter Desk A", "admin@filters-a.example.com")
	tokenB := createSession(t, api, service, "Filter Desk B", "admin@filters-b.example.com")
	deskA := seedFilterDesk(t, api, tokenA, pool, "a")
	deskB := seedFilterDesk(t, api, tokenB, pool, "b")
	for _, query := range []string{
		"contact=" + deskB.ada, "assignee=" + deskB.teammate, "assignee=" + deskB.agent,
		"tag=" + deskB.billing, "tag=" + deskB.billing + "," + deskB.vip + "&tag_match=all",
	} {
		if got := deskA.list(t, api, query); len(got) != 0 {
			t.Fatalf("workspace A ?%s lists %v", query, got)
		}
	}
	for _, query := range []string{
		"", "search=b", "search=builders", "organization=Analytical%20Engines%20Ltd", "awaiting=team", "has_pending_draft=true",
		"delivery_failed=true", "channel=email,web_form,imported,agent,problem_report", "sort=waiting_longest", "sort=priority",
	} {
		for _, name := range deskA.list(t, api, query) {
			if strings.HasPrefix(name, "?") {
				t.Fatalf("workspace A ?%s lists a conversation it does not own: %s", query, name)
			}
		}
	}
}
