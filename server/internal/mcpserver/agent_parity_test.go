package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// mcpToolPending lists routes an agent token may call that no MCP tool calls yet, each with the
// issue that adds the tool. This list may only shrink: a new route that tokens may call ships with
// its MCP tool in the same pull request. When a tool starts calling a route, remove it here.
var mcpToolPending = map[string]int{
	"GET /api/v1/saved-replies": 129,
}

// The gap lists only shrink: these ceilings are their current sizes. Lower a ceiling
// when a gap closes; raising one needs the maintainer's approval written in the pull request.
// PersonOnly rows are not a gap (a person's decision on an agent request stays a person's), but they
// are capped too, so the class cannot become a way to close a route without saying so.
const (
	maxMCPToolPending = 1
	maxClosedToTokens = 17 // AdminTier + TierPending rows
	maxPersonOnly     = 2
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
	closedReasons := []string{httpapi.AdminTier, httpapi.TierPending, httpapi.PersonOnly, httpapi.NotForPeople}
	// A token calls a route directly at its tier or above, and gets an agent request below it or on an
	// Approval route: either way an MCP tool must reach it.
	callable := func(access httpapi.TokenAccess) bool { return access.Tier != "" || access.Approval }

	for key, route := range routes {
		access, ok := table[key]
		if !ok {
			t.Errorf("%s is not classified for agent tokens: add it to tokenTiers in %s with a tier "+
				"(read, draft, send), Approval, or a closed reason (AdminTier, TierPending, PersonOnly, NotForPeople) and its tracking issue", key, tokenScopeFile)
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
		case access.Approval && (access.Tier != "" || access.Closed != ""):
			t.Errorf("%s needs approval for every tier in %s, so it has no tier and no closed reason: drop them", key, tokenScopeFile)
		case access.Tier != "" && !slices.Contains(tiers, access.Tier):
			t.Errorf("%s has tier %q in %s, want one of %v", key, access.Tier, tokenScopeFile, tiers)
		case !callable(access) && !slices.Contains(closedReasons, access.Closed):
			t.Errorf("%s is closed for reason %q in %s, want one of %v", key, access.Closed, tokenScopeFile, closedReasons)
		case (access.Closed == httpapi.AdminTier || access.Closed == httpapi.TierPending) && access.Tracking == 0:
			t.Errorf("%s is closed (%s) in %s without the issue that opens it: set Tracking", key, access.Closed, tokenScopeFile)
		case access.Closed == httpapi.TierPending && !slices.Contains(auth.ScopeNames, access.Target):
			t.Errorf("%s is TierPending in %s without the tier it opens to: set Target", key, tokenScopeFile)
		case access.Closed == httpapi.PersonOnly && strings.TrimSpace(access.Why) == "":
			t.Errorf("%s is PersonOnly in %s without saying why a person must do it: set Why", key, tokenScopeFile)
		}
		if !callable(access) {
			continue
		}
		tool, isCovered := covered[key]
		_, isPending := mcpToolPending[key]
		if !isCovered && !isPending {
			t.Errorf("%s is open to agent tokens (tier %q, approval %v) but no MCP tool calls it: add a tool in server/internal/mcpserver/catalog*.go "+
				"that lists it in Spec.REST (or, only with a tracking issue, add it to mcpToolPending in %s)", key, access.Tier, access.Approval, pendingFile)
		}
		if isCovered && isPending {
			t.Errorf("%s is called by MCP tool %s: remove it from mcpToolPending in %s", key, tool, pendingFile)
		}
	}
	for key, issue := range mcpToolPending {
		if access, ok := table[key]; !ok || !callable(access) {
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
	closed, personOnly := 0, 0
	for _, access := range table {
		switch {
		case access.Closed == httpapi.AdminTier || access.Closed == httpapi.TierPending:
			closed++
		case access.Closed == httpapi.PersonOnly:
			personOnly++
		}
	}
	if closed > maxClosedToTokens {
		t.Errorf("%d routes in %s are closed to every token, over the ceiling of %d: classify the new route for a tier, or Approval, instead",
			closed, tokenScopeFile, maxClosedToTokens)
	}
	if personOnly > maxPersonOnly {
		t.Errorf("%d routes in %s are PersonOnly, over the ceiling of %d: a route a person can do is for agents too, at a tier or as an agent request",
			personOnly, tokenScopeFile, maxPersonOnly)
	}
	for rest, tool := range covered {
		if access := table[rest]; !callable(access) {
			t.Errorf("MCP tool %s calls %s, which is closed to every token in %s: open it for the tool's tier or drop it from Spec.REST", tool, rest, tokenScopeFile)
		}
	}
}
