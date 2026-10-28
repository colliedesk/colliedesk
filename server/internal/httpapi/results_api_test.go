package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/jackc/pgx/v5/pgxpool"
)

type resultsPage struct {
	Current *struct {
		ID      string `json:"id"`
		Windows map[string]struct {
			FirstResponse struct {
				MedianSeconds *int64 `json:"median_seconds"`
				Answered      int    `json:"answered"`
				Waiting       int    `json:"waiting"`
			} `json:"first_response"`
		} `json:"windows"`
	} `json:"current"`
	Baseline      *struct{ ID string } `json:"baseline"`
	BaselineState string               `json:"baseline_state"`
	Error         *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// seedCustomerConversations adds customer-started email conversations created hours ago, each answered
// after the minutes given (0: not yet), and returns their ids.
func seedCustomerConversations(t *testing.T, pool *pgxpool.Pool, workspace string, replies ...int) []string {
	t.Helper()
	ctx := context.Background()
	var contact string
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey',$2) RETURNING id::text`, workspace, workspace+"@customer.example").Scan(&contact); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for index, minutes := range replies {
		created := time.Now().UTC().Add(-time.Duration(index+2) * time.Hour)
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,created_at,updated_at) VALUES($1,$2,'Help','email',$3,$3) RETURNING id::text`,
			workspace, contact, created).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'incoming','Hi',$3)`, workspace, id, created); err != nil {
			t.Fatal(err)
		}
		if minutes > 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'outgoing','On it',$3)`,
				workspace, id, created.Add(time.Duration(minutes)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, id)
	}
	return ids
}

func listedIDs(t *testing.T, api http.Handler, token, query string) (int, []string) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		return response.Code, nil
	}
	var page struct {
		Items []struct{ ID string } `json:"items"`
	}
	decode(t, response, &page)
	var ids []string
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return response.Code, ids
}

// Every role reads the stored numbers, and each number opens exactly the conversations it counts, in any
// status. Another workspace's snapshot, or one no longer kept, answers 410 and lists nothing.
func TestResultsPageNumbersOpenTheConversationsTheyCount(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Results Desk", "admin@results.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "agent@results.example.com", "agent")
	agent := redeemLabelled(t, api, inviteURL, "Agent")
	other := createSession(t, api, service, "Other Results Desk", "admin@other-results.example.com")
	workspace, otherWorkspace := workspaceID(t, api, admin), workspaceID(t, api, other)

	// Nothing computed yet: no numbers, no baseline.
	var page resultsPage
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.Current != nil || page.Baseline != nil || page.BaselineState != "none" {
		t.Fatalf("before any compute = %+v", page)
	}

	ids := seedCustomerConversations(t, pool, workspace, 10, 20, 30, 40, 50, 0)
	seedCustomerConversations(t, pool, otherWorkspace, 5)
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET status='resolved' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}
	runResultsWorker(t, pool)

	for _, token := range []string{admin, agent} {
		page = resultsPage{}
		decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, token, nil), &page)
		month := page.Current.Windows["30d"].FirstResponse
		// Replies after 10 20 30 40 50 minutes: median 30 minutes; one still waiting.
		if page.Current == nil || month.Answered != 5 || month.Waiting != 1 || month.MedianSeconds == nil || *month.MedianSeconds != 1800 || page.BaselineState != "none" {
			t.Fatalf("results = %+v (%+v)", page, month)
		}
	}
	snapshot := page.Current.ID

	code, listed := listedIDs(t, api, admin, "result="+snapshot+":30d.first_response")
	want := append([]string(nil), ids[:5]...)
	sort.Strings(want)
	if code != http.StatusOK || len(listed) != 5 || listed[0] != want[0] || listed[4] != want[4] {
		t.Fatalf("answered list = %d %v, want %v", code, listed, want)
	}
	if code, listed = listedIDs(t, api, agent, "result="+snapshot+":30d.first_response_waiting"); code != http.StatusOK || len(listed) != 1 || listed[0] != ids[5] {
		t.Fatalf("waiting list = %d %v, want %s", code, listed, ids[5])
	}

	// The other workspace cannot open this snapshot, and its own numbers hold only its conversation.
	if code, listed = listedIDs(t, api, other, "result="+snapshot+":30d.first_response"); code != http.StatusGone || listed != nil {
		t.Fatalf("another workspace's snapshot = %d %v, want 410 and nothing", code, listed)
	}
	page = resultsPage{}
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, other, nil), &page)
	if page.Current == nil || page.Current.ID == snapshot || page.Current.Windows["30d"].FirstResponse.Answered != 1 {
		t.Fatalf("other workspace results = %+v", page.Current)
	}

	for _, bad := range []string{"not-a-snapshot", snapshot + ":90d.first_response", snapshot + ":30d.first response"} {
		if response := request(t, api, http.MethodGet, "/api/v1/conversations?result="+url.QueryEscape(bad), nil, admin, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("result=%q answered %d, want 400", bad, response.Code)
		}
	}

	// A snapshot no longer kept answers 410 with the next step.
	if _, err := pool.Exec(context.Background(), `DELETE FROM results_snapshots WHERE id=$1`, snapshot); err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodGet, "/api/v1/conversations?result="+snapshot+":30d.first_response", nil, admin, nil)
	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"results_refreshed"`) || !strings.Contains(response.Body.String(), "Open Results again") {
		t.Fatalf("pruned snapshot = %d %s", response.Code, response.Body.String())
	}
}

// A failed compute is shown to admins only.
func TestResultsComputeErrorIsForAdmins(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Error Desk", "admin@results-error.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "agent@results-error.example.com", "agent")
	agent := redeemLabelled(t, api, inviteURL, "Agent")
	if _, err := pool.Exec(context.Background(), `INSERT INTO results_state(workspace_id,error,error_at) VALUES($1,'statement timeout',now())`, workspaceID(t, api, admin)); err != nil {
		t.Fatal(err)
	}
	var page resultsPage
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.Error == nil || page.Error.Message != "statement timeout" {
		t.Fatalf("admin error = %+v", page.Error)
	}
	page = resultsPage{}
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, agent, nil), &page)
	if page.Error != nil {
		t.Fatalf("agent sees the error %+v", page.Error)
	}
}

func runResultsWorker(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := results.NewWorker(database.New(pool)).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The baseline leaves with the export: the import it comes from, its anchor and status, and its numbers.
func TestResultsBaselineLeavesWithTheExport(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Export Results Desk", "admin@export-results.example.com")
	workspace := workspaceID(t, api, admin)
	if _, err := pool.Exec(context.Background(), `INSERT INTO results_state(workspace_id,baseline_source,baseline_anchor_at,baseline_import_status,baseline_dirty)
		VALUES($1,'freshdesk:account:9',now() - interval '3 days','UNVERIFIED',true)`, workspace); err != nil {
		t.Fatal(err)
	}
	runResultsWorker(t, pool)
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export = %d", response.Code)
	}
	lines := ndjsonLines(t, unzip(t, response.Body.Bytes())["results-baselines.ndjson"])
	if len(lines) != 1 || lines[0]["source"] != "freshdesk:account:9" || lines[0]["import_status"] != "UNVERIFIED" || lines[0]["anchor_at"] == nil {
		t.Fatalf("results-baselines.ndjson = %v", lines)
	}
	numbers, _ := lines[0]["numbers"].(map[string]any)
	if numbers == nil || numbers["windows"] == nil {
		t.Fatalf("exported baseline numbers = %v", lines[0]["numbers"])
	}
}
