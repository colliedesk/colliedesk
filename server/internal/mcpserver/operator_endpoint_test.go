package mcpserver_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// operatorToolNames is what an operator token sees on /mcp/operator, written out by hand.
var operatorToolNames = []string{
	"colliedesk_operator_audit",
	"colliedesk_operator_config_change",
	"colliedesk_operator_config_check",
	"colliedesk_operator_config_get",
	"colliedesk_operator_config_history",
	"colliedesk_operator_config_restore",
	"colliedesk_operator_invite",
	"colliedesk_operator_product_add",
	"colliedesk_operator_product_on_sale",
	"colliedesk_operator_products",
	"colliedesk_operator_request_cancel",
	"colliedesk_operator_request_get",
	"colliedesk_operator_requests",
	"colliedesk_operator_revoke",
	"colliedesk_operator_session_revoke",
	"colliedesk_operator_sign_out",
	"colliedesk_operator_status",
	"colliedesk_operator_team",
	"colliedesk_operator_token_create",
	"colliedesk_operator_trial_confirm",
	"colliedesk_operator_trial_revert",
	"colliedesk_operator_whoami",
}

func operatorToken(t *testing.T, e *env) string {
	t.Helper()
	service := auth.NewOperatorService(database.New(e.pool), tokenseal.Keys{}, testOrigin+"/operator", nil)
	if _, err := service.Pair(context.Background(), "ops@mcp.example.com", false, "tests"); err != nil {
		t.Fatal(err)
	}
	principal, err := service.CLIPrincipal(context.Background(), "ops@mcp.example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := service.CreateToken(context.Background(), principal, "MCP test", nil)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func status(t *testing.T, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// The operator tools live on their own endpoint, for operator tokens only; /mcp never lists or runs them.
func TestOperatorToolsAreServedOnlyOnTheOperatorEndpoint(t *testing.T) {
	e := newEnv(t)
	operator := operatorToken(t, e)
	workspace := e.newWorkspace("Operator Realm MCP", "admin@realm-mcp.example.com").adminTier
	if got := status(t, e.server.URL+mcpserver.OperatorPath, workspace); got != http.StatusUnauthorized {
		t.Fatalf("a workspace token on %s: %d", mcpserver.OperatorPath, got)
	}
	if got := status(t, e.server.URL+mcpserver.Path, operator); got != http.StatusUnauthorized {
		t.Fatalf("an operator token on %s: %d", mcpserver.Path, got)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "colliedesk-test", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: e.server.URL + mcpserver.OperatorPath, HTTPClient: &http.Client{Transport: bearer{operator}}, DisableStandaloneSSE: true, MaxRetries: -1}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, operatorToolNames) {
		t.Fatalf("operator tools = %v", names)
	}
	me := e.ok(session, "colliedesk_operator_whoami", nil)
	if at(t, me, "operator", "email") != "ops@mcp.example.com" {
		t.Fatalf("whoami = %v", me)
	}
	asked := e.ok(session, "colliedesk_operator_token_create", map[string]any{"label": "another client", "note": "for a second AI host"})
	if at(t, asked, "agent_request", "status") != "pending" || at(t, asked, "agent_request", "kind") != "operator" {
		t.Fatalf("token_create = %v", asked)
	}
}
