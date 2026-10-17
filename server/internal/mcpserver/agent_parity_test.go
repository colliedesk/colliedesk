package mcpserver_test

import (
	"slices"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// mcpToolPending lists routes an agent token may call that no MCP tool calls yet, each with the
// issue that adds the tool. This list may only shrink: a new route that tokens may call ships with
// its MCP tool in the same pull request. When a tool starts calling a route, remove it here.
var mcpToolPending = map[string]int{
	"POST /api/v1/auth/logout":                                   131,
	"GET /api/v1/inbox/activity":                                 131,
	"GET /api/v1/conversations/{id}/time-entries":                131,
	"GET /api/v1/attachments/{id}":                               131,
	"GET /api/v1/contacts/{id}":                                  131,
	"PATCH /api/v1/conversations":                                131,
	"POST /api/v1/conversations":                                 131,
	"POST /api/v1/contacts":                                      131,
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": 131,
	"PATCH /api/v1/tags/{id}":                                    131,
	"DELETE /api/v1/tags/{id}":                                   131,
	"GET /api/v1/saved-replies":                                  129,
}

// The gap lists only shrink: these ceilings are their sizes when the gate went in. Lower a ceiling
// when a gap closes; raising one needs the maintainer's approval written in the pull request.
const (
	maxMCPToolPending = 12
	maxClosedToTokens = 25 // ApprovalRequired + AdminTier + TierPending rows
)

const (
	tokenScopeFile = "server/internal/httpapi/token-scope.go"
	pendingFile    = "server/internal/mcpserver/agent_parity_test.go"
)

// Everything a person can do in the web app must be doable by an AI agent: each route is classified
// for agent tokens, and each route a token may call is reached by an MCP tool or tracked as a gap.
func TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool(t *testing.T) {
	table := httpapi.TokenAccessTable()
	routes := map[string]httpapi.Route{}
	for _, route := range httpapi.Routes() {
		routes[route.Method+" "+route.Pattern] = route
	}
	covered := map[string]string{}
	for _, spec := range mcpserver.Catalog() {
		for _, rest := range spec.REST {
			covered[rest] = spec.Name
		}
	}
	tiers := append(slices.Clone(auth.ScopeNames), "handler")
	closedReasons := []string{httpapi.ApprovalRequired, httpapi.AdminTier, httpapi.TierPending, httpapi.NotForPeople}

	for key, route := range routes {
		access, ok := table[key]
		if !ok {
			t.Errorf("%s is not classified for agent tokens: add it to tokenTiers in %s with a tier "+
				"(read, draft, send) or a closed reason (ApprovalRequired, AdminTier, TierPending, NotForPeople) and its tracking issue", key, tokenScopeFile)
			continue
		}
		noSession := route.Permission == auth.PermissionPublic || route.Permission == auth.PermissionChannelInbound
		if noSession && access.Closed != httpapi.NotForPeople {
			t.Errorf("%s takes no session, so no token reaches it: classify it NotForPeople in %s", key, tokenScopeFile)
		}
	}
	for key, access := range table {
		if _, ok := routes[key]; !ok {
			t.Errorf("tokenTiers in %s classifies %s, which is not a route in Routes(): remove the stale row", tokenScopeFile, key)
		}
		switch {
		case access.Tier != "" && access.Closed != "":
			t.Errorf("%s is both open (%s) and closed (%s) in %s: keep one", key, access.Tier, access.Closed, tokenScopeFile)
		case access.Tier != "" && !slices.Contains(tiers, access.Tier):
			t.Errorf("%s has tier %q in %s, want one of %v", key, access.Tier, tokenScopeFile, tiers)
		case access.Tier == "" && !slices.Contains(closedReasons, access.Closed):
			t.Errorf("%s is closed for reason %q in %s, want one of %v", key, access.Closed, tokenScopeFile, closedReasons)
		case access.Closed != "" && access.Closed != httpapi.NotForPeople && access.Tracking == 0:
			t.Errorf("%s is closed (%s) in %s without the issue that opens it: set Tracking", key, access.Closed, tokenScopeFile)
		case access.Closed == httpapi.TierPending && !slices.Contains(auth.ScopeNames, access.Target):
			t.Errorf("%s is TierPending in %s without the tier it opens to: set Target", key, tokenScopeFile)
		}
		if access.Tier == "" {
			continue
		}
		tool, isCovered := covered[key]
		_, isPending := mcpToolPending[key]
		if !isCovered && !isPending {
			t.Errorf("%s is open to agent tokens (tier %s) but no MCP tool calls it: add a tool in server/internal/mcpserver/catalog*.go "+
				"that lists it in Spec.REST (or, only with a tracking issue, add it to mcpToolPending in %s)", key, access.Tier, pendingFile)
		}
		if isCovered && isPending {
			t.Errorf("%s is called by MCP tool %s: remove it from mcpToolPending in %s", key, tool, pendingFile)
		}
	}
	for key, issue := range mcpToolPending {
		if access, ok := table[key]; !ok || access.Tier == "" {
			t.Errorf("mcpToolPending in %s lists %s, which tokens cannot call: remove it", pendingFile, key)
		}
		if issue == 0 {
			t.Errorf("mcpToolPending in %s lists %s without a tracking issue", pendingFile, key)
		}
	}
	if len(mcpToolPending) > maxMCPToolPending {
		t.Errorf("mcpToolPending in %s has %d routes, over the ceiling of %d: ship the MCP tool with the route instead of adding a gap",
			pendingFile, len(mcpToolPending), maxMCPToolPending)
	}
	closed := 0
	for _, access := range table {
		if access.Tier == "" && access.Closed != httpapi.NotForPeople {
			closed++
		}
	}
	if closed > maxClosedToTokens {
		t.Errorf("%d routes in %s are closed to every token, over the ceiling of %d: classify the new route for a tier instead",
			closed, tokenScopeFile, maxClosedToTokens)
	}
	for rest, tool := range covered {
		if access := table[rest]; access.Tier == "" {
			t.Errorf("MCP tool %s calls %s, which is closed to every token in %s: open it for the tool's tier or drop it from Spec.REST", tool, rest, tokenScopeFile)
		}
	}
}
