package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// importCanary is an API key no answer, row, log line or agent request may hold in clear.
const importCanary = "fd-canary-7f3a-NEVER-STORED-IN-CLEAR"

func newImportDesk(t *testing.T) (http.Handler, string, string, string, importJobs, func(string, ...any) int) {
	t.Helper()
	api, service, pool, _ := testAPIWithConfig(t, nil)
	adminA := createSession(t, api, service, "Import Alpha", "admin@import-alpha.example.com")
	adminB := createSession(t, api, service, "Import Bravo", "admin@import-bravo.example.com")
	agentA := joinAsAgent(t, api, adminA, "agent@import-alpha.example.com")
	jobs := seedImportJobs(t, pool, workspaceID(t, api, adminA))
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return api, adminA, adminB, agentA, jobs, count
}

func TestAnImportPreviewIsAcceptedAndNoAnswerCarriesTheKey(t *testing.T) {
	logs := captureLogs(t)
	api, admin, _, _, _, count := newImportDesk(t)
	response := request(t, api, http.MethodPost, "/api/v1/imports",
		map[string]any{"source": "freshdesk", "domain": "https://Beta.freshdesk.com/", "api_key": importCanary, "options": map[string]any{"recent_days": 30}}, admin, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var job struct {
		ID, State, Domain string
		HasKey            bool `json:"has_key"`
		Options           struct {
			RecentDays int `json:"recent_days"`
		}
	}
	decode(t, response, &job)
	if job.State != "previewing" || job.Domain != "beta.freshdesk.com" || !job.HasKey || job.Options.RecentDays != 30 {
		t.Fatalf("job = %+v", job)
	}
	answers := []string{response.Body.String()}
	for _, path := range []string{"/api/v1/imports", "/api/v1/imports/" + job.ID} {
		read := request(t, api, http.MethodGet, path, nil, admin, nil)
		if read.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, read.Code)
		}
		answers = append(answers, read.Body.String())
	}
	for _, answer := range answers {
		if strings.Contains(answer, importCanary) || strings.Contains(answer, "sealed") {
			t.Fatalf("an answer carries the key: %s", answer)
		}
	}
	if n := count(`SELECT count(*) FROM import_jobs WHERE position($1::bytea in sealed_key) > 0 OR options::text LIKE '%'||$2||'%'`, []byte(importCanary), importCanary); n != 0 {
		t.Fatal("the key is stored in clear")
	}
	if strings.Contains(logs.String(), importCanary) {
		t.Fatal("the key reached the log")
	}
	if notReady := request(t, api, http.MethodGet, "/api/v1/imports/"+job.ID+"/report", nil, admin, nil); notReady.Code != http.StatusNotFound ||
		!strings.Contains(notReady.Body.String(), "report_not_ready") {
		t.Fatalf("report of a preview: %d %s", notReady.Code, notReady.Body.String())
	}
}

func TestAnImportNeedsAFreshdeskAddressAndAKey(t *testing.T) {
	api, admin, _, _, _, _ := newImportDesk(t)
	for _, c := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"source": "freshdesk", "domain": "acme.zendesk.com", "api_key": "k"}, "freshdesk_domain_invalid"},
		{map[string]any{"source": "freshdesk", "domain": "acme.freshdesk.com", "api_key": "  "}, "freshdesk_key_missing"},
		{map[string]any{"source": "zendesk", "domain": "acme.freshdesk.com", "api_key": "k"}, "invalid_request"},
		{map[string]any{"source": "freshdesk", "domain": "acme.freshdesk.com", "api_key": "k", "options": map[string]any{"recent_days": 366}}, "invalid_request"},
	} {
		response := request(t, api, http.MethodPost, "/api/v1/imports", c.body, admin, nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), c.code) {
			t.Errorf("%v: status=%d body=%s, want 400 %s", c.body, response.Code, response.Body.String(), c.code)
		}
	}
}

func TestAKeyNeverWaitsInAnAgentRequest(t *testing.T) {
	api, admin, _, _, jobs, count := newImportDesk(t)
	for _, tier := range []string{"read", "draft", "send"} {
		token := issueToken(t, api, admin, "import "+tier, tier).Token
		for _, call := range []struct{ method, path string }{{http.MethodPost, "/api/v1/imports"}, {http.MethodPut, "/api/v1/imports/" + jobs.waitingKey + "/key"}} {
			body := map[string]any{"source": "freshdesk", "domain": "acme.freshdesk.com", "api_key": importCanary}
			if call.method == http.MethodPut {
				body = map[string]any{"api_key": importCanary}
			}
			response := request(t, api, call.method, call.path, body, token, nil)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "credentials_need_admin_token") {
				t.Fatalf("%s %s with a %s token: %d %s", call.method, call.path, tier, response.Code, response.Body.String())
			}
		}
	}
	if n := count(`SELECT count(*) FROM agent_requests WHERE body::text LIKE '%'||$1||'%'`, importCanary); n != 0 {
		t.Fatalf("%d agent requests hold the key", n)
	}
	admin2 := issueToken(t, api, admin, "import admin", "admin").Token
	if response := request(t, api, http.MethodPost, "/api/v1/imports", map[string]any{"source": "freshdesk", "domain": "acme.freshdesk.com", "api_key": importCanary}, admin2, nil); response.Code != http.StatusAccepted {
		t.Fatalf("an admin token: %d %s", response.Code, response.Body.String())
	}
}

func TestAnUndoByAnAgentTokenWaitsForAPerson(t *testing.T) {
	api, admin, _, _, jobs, count := newImportDesk(t)
	token := issueToken(t, api, admin, "import admin", "admin").Token
	response := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.finished+"/undo", nil, token, nil)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "request_id") {
		t.Fatalf("status=%d body=%s, want an agent request", response.Code, response.Body.String())
	}
	if n := count(`SELECT count(*) FROM import_jobs WHERE kind='undo'`); n != 0 {
		t.Fatal("an undo was queued before a person approved it")
	}
	request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.waitingKey+"/cancel", nil, admin, nil) // it holds the workspace
	direct := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.finished+"/undo", nil, admin, nil)
	if direct.Code != http.StatusAccepted || !strings.Contains(direct.Body.String(), `"kind":"undo"`) {
		t.Fatalf("an admin's undo: %d %s", direct.Code, direct.Body.String())
	}
}

func TestOnlyAdminsImport(t *testing.T) {
	api, _, _, agent, jobs, _ := newImportDesk(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/imports"}, {http.MethodPost, "/api/v1/imports"}, {http.MethodGet, "/api/v1/imports/" + jobs.finished},
		{http.MethodPost, "/api/v1/imports/" + jobs.previewed + "/start"}, {http.MethodPost, "/api/v1/imports/" + jobs.previewed + "/cancel"},
		{http.MethodPut, "/api/v1/imports/" + jobs.waitingKey + "/key"}, {http.MethodGet, "/api/v1/imports/" + jobs.finished + "/report"},
		{http.MethodPost, "/api/v1/imports/" + jobs.finished + "/undo"},
	} {
		response := request(t, api, call.method, call.path, map[string]any{"source": "freshdesk", "domain": "acme.freshdesk.com", "api_key": "k"}, agent, nil)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s by an agent: %d %s, want 403", call.method, call.path, response.Code, response.Body.String())
		}
	}
}

func TestAnotherWorkspaceCannotReachAnImport(t *testing.T) {
	api, adminA, adminB, _, jobs, _ := newImportDesk(t)
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/imports/" + jobs.finished, nil},
		{http.MethodPost, "/api/v1/imports/" + jobs.previewed + "/start", nil},
		{http.MethodPost, "/api/v1/imports/" + jobs.previewed + "/cancel", nil},
		{http.MethodPut, "/api/v1/imports/" + jobs.waitingKey + "/key", map[string]any{"api_key": "k"}},
		{http.MethodGet, "/api/v1/imports/" + jobs.finished + "/report", nil},
		{http.MethodGet, "/api/v1/imports/" + jobs.finished + "/report?format=markdown", nil},
		{http.MethodPost, "/api/v1/imports/" + jobs.finished + "/undo", nil},
	} {
		response := request(t, api, call.method, call.path, call.body, adminB, nil)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s from another workspace: %d %s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	var list struct {
		Items []any `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/imports", nil, adminB, nil), &list)
	if len(list.Items) != 0 {
		t.Fatalf("another workspace lists %d imports", len(list.Items))
	}
	var job struct{ State string }
	decode(t, request(t, api, http.MethodGet, "/api/v1/imports/"+jobs.previewed, nil, adminA, nil), &job)
	if job.State != "previewed" {
		t.Fatalf("the job changed: %+v", job)
	}
}

func TestTheReportDownloadsAsMarkdownAndOneImportRunsAtATime(t *testing.T) {
	api, admin, _, _, jobs, _ := newImportDesk(t)
	markdown := request(t, api, http.MethodGet, "/api/v1/imports/"+jobs.finished+"/report?format=markdown", nil, admin, nil)
	if markdown.Code != http.StatusOK || !strings.HasPrefix(markdown.Header().Get("Content-Type"), "text/markdown") ||
		!strings.Contains(markdown.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(markdown.Body.String(), "# Freshdesk import: OK") {
		t.Fatalf("markdown: %d %v %s", markdown.Code, markdown.Header(), markdown.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.previewed+"/start", nil, admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "import_running") {
		// The waiting_key job of the fixture holds the workspace.
		t.Fatalf("start while another waits: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.waitingKey+"/cancel", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.previewed+"/start", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("start: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/imports/"+jobs.finished+"/start", nil, admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "import_state") {
		t.Fatalf("start a finished import: %d %s", response.Code, response.Body.String())
	}
}

func TestTheExportKeepsImportsButNeverTheirKey(t *testing.T) {
	api, admin, _, _, jobs, _ := newImportDesk(t)
	if response := request(t, api, http.MethodPost, "/api/v1/imports", map[string]any{"source": "freshdesk", "domain": "beta.freshdesk.com", "api_key": importCanary}, admin, nil); response.Code != http.StatusAccepted {
		t.Fatalf("preview: %d %s", response.Code, response.Body.String())
	}
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export: %d %s", response.Code, response.Body.String())
	}
	entries := unzip(t, response.Body.Bytes())
	for name, data := range entries {
		if strings.Contains(string(data), importCanary) {
			t.Fatalf("%s holds the Freshdesk key", name)
		}
	}
	lines := ndjsonLines(t, entries["import-jobs.ndjson"])
	if len(lines) != 4 || !strings.Contains(string(entries["import-jobs.ndjson"]), jobs.finished) || !strings.Contains(string(entries["import-jobs.ndjson"]), `"report":{`) {
		t.Fatalf("import-jobs.ndjson = %s", entries["import-jobs.ndjson"])
	}
}
