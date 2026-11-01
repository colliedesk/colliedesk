package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

const bulkPath = "/api/v1/conversations"

type bulkFixture struct {
	api      http.Handler
	token    string
	agentID  string
	other    string // conversation in another workspace
	otherTok string
	foreign  string // agent of another workspace
	ids      []string
}

func newBulkFixture(t *testing.T, count int) bulkFixture {
	t.Helper()
	api, service := testAPI(t)
	return seedBulkFixture(t, api, service, count)
}

func seedBulkFixture(t *testing.T, api http.Handler, service *auth.Service, count int) bulkFixture {
	t.Helper()
	token := createSession(t, api, service, "Bulk Alpha", "admin@bulk-alpha.example.com")
	otherToken := createSession(t, api, service, "Bulk Beta", "admin@bulk-beta.example.com")
	f := bulkFixture{api: api, token: token, otherTok: otherToken}
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	f.agentID = me.Agent.ID
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, otherToken, nil), &me)
	f.foreign = me.Agent.ID
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Bulk", "email": "bulk@customer.example.com"})
	for i := 0; i < count; i++ {
		f.ids = append(f.ids, create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": fmt.Sprintf("Bulk %d", i)}))
	}
	otherContact := create(t, api, otherToken, "/api/v1/contacts", map[string]any{"name": "Other", "email": "other@customer.example.com"})
	f.other = create(t, api, otherToken, "/api/v1/conversations", map[string]any{"contact_id": otherContact, "subject": "Beta only"})
	return f
}

func (f bulkFixture) patch(t *testing.T, ids []string, changes map[string]any) (int, string) {
	t.Helper()
	response := request(t, f.api, http.MethodPatch, bulkPath, map[string]any{"ids": ids, "changes": changes}, f.token, nil)
	return response.Code, response.Body.String()
}

func (f bulkFixture) snapshot(t *testing.T, ids ...string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, id := range ids {
		token := f.token
		if id == f.other {
			token = f.otherTok
		}
		response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil)
		result[id] = response.Body.String()
	}
	return result
}

func (f bulkFixture) assertUnchanged(t *testing.T, before map[string]string) {
	t.Helper()
	ids := make([]string, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	for id, body := range f.snapshot(t, ids...) {
		if body != before[id] {
			t.Errorf("conversation %s changed by a refused bulk update:\nbefore=%s\nafter=%s", id, before[id], body)
		}
	}
}

func TestBulkUpdateSetsStatusAssigneeAndTags(t *testing.T) {
	f := newBulkFixture(t, 3)
	tag := create(t, f.api, f.token, "/api/v1/tags", map[string]any{"name": "Bulk tag"})
	untouchedBefore := f.snapshot(t, f.ids[2])
	targets := f.ids[:2]

	code, body := f.patch(t, targets, map[string]any{"status": "resolved"})
	if code != http.StatusOK || body != `{"told":0,"updated":2}`+"\n" {
		t.Fatalf("set status: status=%d body=%q, want 200 {\"updated\":2}", code, body)
	}
	code, body = f.patch(t, targets, map[string]any{"assignee_id": f.agentID})
	if code != http.StatusOK {
		t.Fatalf("assign: status=%d body=%s", code, body)
	}
	code, body = f.patch(t, targets, map[string]any{"add_tag_ids": []string{tag}})
	if code != http.StatusOK {
		t.Fatalf("add tag: status=%d body=%s", code, body)
	}
	for _, id := range targets {
		var item struct {
			Status     string  `json:"status"`
			AssigneeID *string `json:"assignee_id"`
		}
		response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+id, nil, f.token, nil)
		decode(t, response, &item)
		if item.Status != "resolved" || item.AssigneeID == nil || *item.AssigneeID != f.agentID || !bodyHasTag(t, response, tag) {
			t.Errorf("conversation %s = %s, want resolved, assigned to the caller and tagged", id, response.Body.String())
		}
	}
	f.assertUnchanged(t, untouchedBefore)

	code, body = f.patch(t, targets, map[string]any{"assignee_id": nil, "remove_tag_ids": []string{tag}, "status": "open"})
	if code != http.StatusOK {
		t.Fatalf("unassign and untag: status=%d body=%s", code, body)
	}
	for _, id := range targets {
		var item struct {
			Status     string  `json:"status"`
			AssigneeID *string `json:"assignee_id"`
		}
		response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+id, nil, f.token, nil)
		decode(t, response, &item)
		if item.Status != "open" || item.AssigneeID != nil || bodyHasTag(t, response, tag) {
			t.Errorf("conversation %s = %s, want open, unassigned and untagged", id, response.Body.String())
		}
	}
}

func updatedAt(t *testing.T, body string) time.Time {
	t.Helper()
	var item struct {
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return item.UpdatedAt
}

func TestBulkUpdateMovesUpdatedAtLikeASingleUpdate(t *testing.T) {
	f := newBulkFixture(t, 2)
	before := f.snapshot(t, f.ids...)
	time.Sleep(20 * time.Millisecond)
	if code, body := f.patch(t, f.ids, map[string]any{"status": "pending"}); code != http.StatusOK {
		t.Fatalf("status=%d body=%s", code, body)
	}
	for id, body := range f.snapshot(t, f.ids...) {
		if then, now := updatedAt(t, before[id]), updatedAt(t, body); !now.After(then) {
			t.Errorf("conversation %s updated_at did not move: %s -> %s", id, then, now)
		}
	}
}

func TestBulkUpdateRefusesMoreThanFiftyIDs(t *testing.T) {
	f := newBulkFixture(t, 1)
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
	}
	ids[0] = f.ids[0]
	before := f.snapshot(t, f.ids[0])
	if code, body := f.patch(t, ids, map[string]any{"status": "closed"}); code != http.StatusBadRequest {
		t.Fatalf("51 ids: status=%d body=%s, want 400", code, body)
	}
	f.assertUnchanged(t, before)
}

func TestBulkUpdateRefusesEmptyRequests(t *testing.T) {
	f := newBulkFixture(t, 1)
	tag := create(t, f.api, f.token, "/api/v1/tags", map[string]any{"name": "Both ways"})
	for name, body := range map[string]map[string]any{
		"no ids":              {"ids": []string{}, "changes": map[string]any{"status": "closed"}},
		"no changes":          {"ids": f.ids, "changes": map[string]any{}},
		"missing changes":     {"ids": f.ids},
		"unknown change":      {"ids": f.ids, "changes": map[string]any{"subject": "x"}},
		"bad status":          {"ids": f.ids, "changes": map[string]any{"status": "deleted"}},
		"duplicate ids":       {"ids": []string{f.ids[0], f.ids[0]}, "changes": map[string]any{"status": "closed"}},
		"bad id":              {"ids": []string{"not-a-uuid"}, "changes": map[string]any{"status": "closed"}},
		"same tag both ways":  {"ids": f.ids, "changes": map[string]any{"add_tag_ids": []string{tag}, "remove_tag_ids": []string{tag}}},
		"assignee wrong type": {"ids": f.ids, "changes": map[string]any{"assignee_id": 7}},
	} {
		t.Run(name, func(t *testing.T) {
			before := f.snapshot(t, f.ids...)
			response := request(t, f.api, http.MethodPatch, bulkPath, body, f.token, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", response.Code, response.Body.String())
			}
			f.assertUnchanged(t, before)
		})
	}
}

func TestBulkUpdateWithAnIDOutsideTheWorkspaceChangesNothing(t *testing.T) {
	f := newBulkFixture(t, 2)
	missing := "99999999-9999-4999-8999-999999999999"
	bodies := map[string]string{}
	for name, foreign := range map[string]string{"other workspace": f.other, "missing": missing} {
		t.Run(name, func(t *testing.T) {
			before := f.snapshot(t, append([]string{f.other}, f.ids...)...)
			code, body := f.patch(t, []string{f.ids[0], foreign, f.ids[1]}, map[string]any{"status": "closed"})
			if code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s, want 404", code, body)
			}
			bodies[name] = body
			f.assertUnchanged(t, before)
		})
	}
	// Every response carries its own request id; the rest of the body must be byte for byte the same.
	if withoutRequestID(bodies["other workspace"]) != withoutRequestID(bodies["missing"]) {
		t.Errorf("404 tells a foreign id from a missing one: %q vs %q", bodies["other workspace"], bodies["missing"])
	}
}

func TestBulkUpdateRejectsInvalidAssigneeAndTagAtomically(t *testing.T) {
	f := newBulkFixture(t, 2)
	foreignTag := create(t, f.api, f.otherTok, "/api/v1/tags", map[string]any{"name": "Beta tag"})
	cases := map[string]map[string]any{
		"assignee of another workspace": {"status": "closed", "assignee_id": f.foreign},
		"unknown assignee":              {"status": "closed", "assignee_id": "99999999-9999-4999-8999-999999999999"},
		"tag of another workspace":      {"status": "closed", "add_tag_ids": []string{foreignTag}},
		"unknown tag to remove":         {"status": "closed", "remove_tag_ids": []string{"99999999-9999-4999-8999-999999999999"}},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			before := f.snapshot(t, f.ids...)
			code, body := f.patch(t, f.ids, changes)
			if code < 400 || code >= 500 || code == http.StatusNotFound {
				t.Fatalf("status=%d body=%s, want a 4xx other than 404", code, body)
			}
			f.assertUnchanged(t, before)
		})
	}
}

func TestBulkUpdateNeedsASessionAndTheWritePermission(t *testing.T) {
	f := newBulkFixture(t, 1)
	before := f.snapshot(t, f.ids...)
	response := request(t, f.api, http.MethodPatch, bulkPath, map[string]any{"ids": f.ids, "changes": map[string]any{"status": "closed"}}, "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status=%d, want 401", response.Code)
	}
	f.assertUnchanged(t, before)

	for _, route := range httpapi.Routes() {
		if route.Method == http.MethodPatch && route.Pattern == bulkPath && route.Permission != auth.PermissionConversationsWrite {
			t.Errorf("bulk update permission=%s, want %s like the single update", route.Permission, auth.PermissionConversationsWrite)
		}
	}
}

func TestBulkUpdateOfFiftyConversationsCostsNoMoreStatementsThanOneAndStaysInBudget(t *testing.T) {
	counter := &queryCounter{}
	pool := testPoolTraced(t, counter)
	// Every conversation is in a chat whose channel tells each status: the notices follow the commit, so
	// they cost the update nothing (docs/internals/performance.md).
	loop := channeltest.NewLoopback(statusChat, channels.Async)
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(statusChat)); err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.ExtraChannelTypes = []channels.Adapter{loop} })
	f := seedBulkFixture(t, api, service, 50)
	tag := create(t, api, f.token, "/api/v1/tags", map[string]any{"name": "Budget tag"})
	chat := create(t, api, f.token, "/api/v1/channels", map[string]any{"type": string(statusChat), "config": map[string]any{"account": "budget"},
		"settings": map[string]any{"notices": map[string]any{"status_pending": "{ref}: waiting.", "status_resolved": "{ref}: fixed.", "status_closed": "{ref}: closed.", "reopened": "{ref}: open."}}})
	if _, err := pool.Exec(context.Background(), `WITH thread AS (
			INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id) SELECT workspace_id,$2::uuid,'direct','user:'||id FROM conversations WHERE id=ANY($1::uuid[]) RETURNING workspace_id,id,external_id),
		item AS (INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,body,disposition,occurred_at)
			SELECT workspace_id,$2::uuid,id,'in','customer',external_id,'conversation',now() FROM thread RETURNING workspace_id,id,body)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT workspace_id,substr(body,6)::uuid,id,'opened' FROM item`, f.ids, chat); err != nil {
		t.Fatal(err)
	}

	run := func(ids []string, status string) (int, time.Duration) {
		counter.mu.Lock()
		counter.statements = 0
		counter.mu.Unlock()
		started := time.Now()
		code, body := f.patch(t, ids, map[string]any{"status": status, "assignee_id": f.agentID, "add_tag_ids": []string{tag}})
		elapsed := time.Since(started)
		if code != http.StatusOK {
			t.Fatalf("status=%d body=%s", code, body)
		}
		counter.mu.Lock()
		defer counter.mu.Unlock()
		return counter.statements, elapsed
	}
	one, _ := run(f.ids[:1], "pending")
	fifty, best := run(f.ids, "resolved")
	if fifty != one {
		t.Errorf("50 ids cost %d statements, 1 id costs %d; the cost must not grow with the ids", fifty, one)
	}
	// The statement count is the deterministic gate. The time is the best of five runs, so one
	// scheduling stall on a shared host does not fail it; the budget itself stays at 100 ms.
	statuses := []string{"pending", "open", "closed", "resolved"}
	for _, status := range statuses {
		if _, elapsed := run(f.ids, status); elapsed < best {
			best = elapsed
		}
	}
	if best > bulkLatencyBudget {
		t.Errorf("best of 5 runs for 50 ids took %s, want at most %s", best, bulkLatencyBudget)
	}
	if marked := queryString(t, pool, `SELECT count(*)::text FROM conversations c WHERE status_notice_due IS NOT NULL
		AND EXISTS(SELECT 1 FROM conversation_thread_items l WHERE l.conversation_id=c.id)`); marked != "50" {
		t.Errorf("chat conversations marked to tell = %s, want 50", marked)
	}
	t.Logf("bulk update of 50 ids: %d statements (1 id: %d), best of 5 %s", fifty, one, best)
}

const bulkLatencyBudget = 100 * time.Millisecond

func TestBulkTagChangeBumpsOnlyConversationsThatChanged(t *testing.T) {
	f := newBulkFixture(t, 2)
	tag := create(t, f.api, f.token, "/api/v1/tags", map[string]any{"name": "Bump"})
	if code, body := f.patch(t, f.ids[:1], map[string]any{"add_tag_ids": []string{tag}}); code != http.StatusOK {
		t.Fatalf("seed tag: status=%d body=%s", code, body)
	}
	before := f.snapshot(t, f.ids...)
	time.Sleep(20 * time.Millisecond)

	if code, body := f.patch(t, f.ids, map[string]any{"add_tag_ids": []string{tag}}); code != http.StatusOK {
		t.Fatalf("add: status=%d body=%s", code, body)
	}
	after := f.snapshot(t, f.ids...)
	if !updatedAt(t, after[f.ids[0]]).Equal(updatedAt(t, before[f.ids[0]])) {
		t.Errorf("adding a tag the conversation already had moved updated_at")
	}
	if !updatedAt(t, after[f.ids[1]]).After(updatedAt(t, before[f.ids[1]])) {
		t.Errorf("adding a new tag did not move updated_at")
	}

	before = after
	time.Sleep(20 * time.Millisecond)
	other := create(t, f.api, f.token, "/api/v1/tags", map[string]any{"name": "Never applied"})
	if code, body := f.patch(t, f.ids, map[string]any{"remove_tag_ids": []string{other}}); code != http.StatusOK {
		t.Fatalf("remove absent: status=%d body=%s", code, body)
	}
	f.assertUnchanged(t, before)
	if code, body := f.patch(t, f.ids[:1], map[string]any{"remove_tag_ids": []string{tag}}); code != http.StatusOK {
		t.Fatalf("remove: status=%d body=%s", code, body)
	}
	after = f.snapshot(t, f.ids...)
	if !updatedAt(t, after[f.ids[0]]).After(updatedAt(t, before[f.ids[0]])) {
		t.Errorf("removing a tag did not move updated_at")
	}
	if !updatedAt(t, after[f.ids[1]]).Equal(updatedAt(t, before[f.ids[1]])) {
		t.Errorf("a conversation outside the id list changed")
	}
}

// pauseAfterTagInsert delays the single-conversation tag insert once it ran, so a concurrent bulk
// update reaches its own tag insert while the single transaction still holds the row it inserted.
type pauseAfterTagInsert struct{ pause time.Duration }

type tagInsertKey struct{}

func (p pauseAfterTagInsert) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, tagInsertKey{}, strings.Contains(data.SQL, "INSERT INTO conversation_tags") && strings.Contains(data.SQL, "VALUES($1,$2,$3)"))
}

func (p pauseAfterTagInsert) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if match, _ := ctx.Value(tagInsertKey{}).(bool); match {
		time.Sleep(p.pause)
	}
}

// A bulk tag change and a single tag change on the same conversation must take the conversation
// row before conversation_tags, or each waits for the other and PostgreSQL aborts one (40P01).
func TestBulkAndSingleTagAddsOnOneConversationDoNotDeadlock(t *testing.T) {
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, pauseAfterTagInsert{pause: 300 * time.Millisecond}), nil)
	f := seedBulkFixture(t, api, service, 2)
	tag := create(t, api, f.token, "/api/v1/tags", map[string]any{"name": "Contended"})

	var wg sync.WaitGroup
	var singleCode, bulkCode int
	var singleBody, bulkBody string
	wg.Add(2)
	go func() {
		defer wg.Done()
		response := request(t, api, http.MethodPut, "/api/v1/conversations/"+f.ids[0]+"/tags/"+tag, nil, f.token, nil)
		singleCode, singleBody = response.Code, response.Body.String()
	}()
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		bulkCode, bulkBody = f.patch(t, f.ids, map[string]any{"add_tag_ids": []string{tag}})
	}()
	wg.Wait()
	if singleCode != http.StatusOK || bulkCode != http.StatusOK {
		t.Fatalf("single PUT status=%d body=%s; bulk status=%d body=%s; want both 200", singleCode, singleBody, bulkCode, bulkBody)
	}
	for _, id := range f.ids {
		if response := request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, f.token, nil); !bodyHasTag(t, response, tag) {
			t.Errorf("conversation %s lacks the tag after both updates", id)
		}
	}
}

func TestBulkAssignToADeactivatedAgentIsRefusedAndNothingChanges(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	f := seedBulkFixture(t, api, service, 2)
	var departed string
	if err := pool.QueryRow(context.Background(), `INSERT INTO agents(workspace_id,email,role,deactivated_at)
		VALUES($1,'gone@bulk-alpha.example.com','agent',now()) RETURNING id::text`, workspaceID(t, api, f.token)).Scan(&departed); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t, f.ids...)
	code, body := f.patch(t, f.ids, map[string]any{"status": "closed", "assignee_id": departed})
	if code < 400 || code >= 500 || code == http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want a 4xx other than 404", code, body)
	}
	f.assertUnchanged(t, before)
}
