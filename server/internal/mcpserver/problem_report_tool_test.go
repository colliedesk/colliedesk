package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestProblemReportToolFilesAConversationInTheSupportDesk(t *testing.T) {
	e := newEnvWith(t, func(config *httpapi.Config) { config.ProblemReportWorkspace = "mcp-support-desk" })
	support := e.newWorkspace("MCP Support Desk", "admin@mcp-support.example.com")
	w := e.newWorkspace("Report Tool Desk", "admin@report-tool.example.com")
	other := e.newWorkspace("Bystander Desk", "admin@bystander.example.com")
	contact := e.created("/api/v1/contacts", map[string]any{"name": "Zed Customer", "email": "zed@private-customer.example.com"}, w.admin)
	e.created("/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Private refund question"}, w.admin)

	read := e.connect(w.read, "")
	out := e.ok(read, "colliedesk_problem_report", map[string]any{
		"description": "Replies fail to send from the agent.",
		"diagnostics": map[string]any{
			"page_path": "/conversations/abc?q=zed",
			"failed_api_calls": []any{map[string]any{"method": "POST", "route": "/api/v1/conversations/{id}/messages", "status": 502,
				"request_id": "feedc0de1234", "at": "2026-10-19T08:00:00Z"}},
		},
	})
	reference, _ := out["reference"].(string)
	if reference == "" {
		t.Fatalf("report result = %v, want a reference", out)
	}

	status, page := e.rest(http.MethodGet, "/api/v1/conversations", nil, support.admin)
	items, _ := page["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("support desk conversations = %v (status %d), want one", page, status)
	}
	id := items[0].(map[string]any)["id"].(string)
	_, thread := e.rest(http.MethodGet, "/api/v1/conversations/"+id+"/messages", nil, support.admin)
	body := stringify(thread)
	for _, want := range []string{"Replies fail to send from the agent.", "feedc0de1234", "agent token (read)", reference, "Report Tool Desk"} {
		if !strings.Contains(body, want) {
			t.Errorf("support message lacks %q: %s", want, body)
		}
	}
	for _, leaked := range []string{"Private refund question", "Zed Customer", "private-customer", "q=zed"} {
		if strings.Contains(body, leaked) || strings.Contains(stringify(page), leaked) {
			t.Errorf("the report leaks %q", leaked)
		}
	}
	if _, theirs := e.rest(http.MethodGet, "/api/v1/conversations", nil, other.admin); len(theirs["items"].([]any)) != 0 {
		t.Errorf("an unrelated workspace sees %v", theirs)
	}
}

func TestProblemReportToolExplainsWhenTheServerHasNoSupportDesk(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Self Hosted Desk", "admin@self-hosted.example.com")
	message := e.fails(e.connect(w.read, ""), "colliedesk_problem_report", map[string]any{"description": "Something broke."})
	if !strings.Contains(message, "does not receive problem reports") || !strings.Contains(message, "github.com/colliedesk/colliedesk/issues") {
		t.Errorf("message = %q, want it to say the server takes no reports and point to GitHub", message)
	}
}

func TestProblemReportToolRefusesAnEmptyDescription(t *testing.T) {
	e := newEnvWith(t, func(config *httpapi.Config) { config.ProblemReportWorkspace = "mcp-empty-desk" })
	e.newWorkspace("MCP Empty Desk", "admin@mcp-empty.example.com")
	w := e.newWorkspace("Empty Report Desk", "admin@empty-report.example.com")
	message := e.fails(e.connect(w.read, ""), "colliedesk_problem_report", map[string]any{"description": "   "})
	if !strings.Contains(strings.ToLower(message), "description") {
		t.Errorf("message = %q, want it to name the description", message)
	}
}
