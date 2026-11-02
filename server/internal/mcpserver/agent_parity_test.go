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
var mcpToolPending = map[string]int{}

// The gap lists only shrink, and both are empty: every route a person reaches needs one permission,
// the same for people and tokens. Raising a ceiling needs the maintainer's approval written in the
// pull request. maxClosedToTokens counts rows closed for any reason other than PersonOnly and
// NotForPeople. PersonOnly rows are not a gap, but they are capped too, so the class cannot become a
// way to close a route without saying so.
const (
	maxMCPToolPending = 0
	maxClosedToTokens = 0
	// Creating an agent token (a token that makes tokens outlives its own revocation), the signed image
	// address of the email view (an agent reads the stored HTML and has no screen), and the operator's
	// decision on an operator request (approve, reject).
	maxPersonOnly = 4
)

const (
	routeAccessFile = "server/internal/httpapi/token-scope.go"
	pendingFile     = "server/internal/mcpserver/agent_parity_test.go"
)

// Everything a person can do in the web app must be doable by an AI agent: each route says what it
// needs (routeAccess), and each route a token may call is reached by an MCP tool or tracked as a gap.
func TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool(t *testing.T) {
	table := httpapi.RouteAccessTable()
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
	permissions := auth.AllPermissions()
	closedReasons := []string{httpapi.PersonOnly, httpapi.NotForPeople}
	operatorRoute := func(key string) bool {
		return strings.Contains(key, " /api/v1/operator/") || strings.Contains(key, "/agent-requests/")
	}
	// A token calls a route that needs a permission, one the handler checks from the body, or one any
	// signed-in caller may use; an operator token calls an operator route, or files a request on one.
	callable := func(access httpapi.TokenAccess) bool {
		return access.Closed == "" && (access.Permission != "" || access.Anyone || access.Tier != "" || access.Approval)
	}

	for key, route := range routes {
		access, ok := table[key]
		if !ok {
			t.Errorf("%s says nothing about what it needs: add it to routeAccess in %s with need(permission), byHandler(permission), "+
				"anyone, personOnly with why, or notForPeople", key, routeAccessFile)
			continue
		}
		noSession := route.Permission == auth.PermissionPublic || route.Permission == auth.PermissionChannelInbound
		if noSession && access.Closed != httpapi.NotForPeople {
			t.Errorf("%s takes no session, so no token reaches it: classify it notForPeople in %s", key, routeAccessFile)
		}
	}
	for key, access := range table {
		if _, ok := routes[key]; !ok {
			t.Errorf("routeAccess in %s classifies %s, which is not a route in Routes(): remove the stale row", routeAccessFile, key)
		}
		workspacePermission := !operatorRoute(key) && access.Permission != ""
		switch {
		case access.Closed != "" && (access.Permission != "" || access.Anyone || access.Approval):
			t.Errorf("%s is both open and closed (%s) in %s: keep one", key, access.Closed, routeAccessFile)
		case access.Anyone && access.Permission != "":
			t.Errorf("%s is open to anyone and needs %s in %s: keep one", key, access.Permission, routeAccessFile)
		case access.Approval && !operatorRoute(key):
			t.Errorf("%s waits for approval in %s, but only operator routes may: a workspace token acts at once with the permission, or is refused", key, routeAccessFile)
		case access.Tier != "" && !operatorRoute(key):
			t.Errorf("%s has tier %q in %s; tiers are gone, only operator routes keep the operator tier", key, access.Tier, routeAccessFile)
		case workspacePermission && !slices.Contains(permissions, access.Permission):
			t.Errorf("%s needs %q in %s, which is not a permission (auth.AllPermissions)", key, access.Permission, routeAccessFile)
		case access.Closed == "" && !callable(access):
			t.Errorf("%s needs nothing and is not open to anyone in %s: say what it needs", key, routeAccessFile)
		case access.Closed != "" && !slices.Contains(closedReasons, access.Closed):
			t.Errorf("%s is closed for reason %q in %s, want one of %v", key, access.Closed, routeAccessFile, closedReasons)
		case access.Closed == httpapi.PersonOnly && strings.TrimSpace(access.Why) == "":
			t.Errorf("%s is personOnly in %s without saying why a person must do it: set Why", key, routeAccessFile)
		case access.PersonNeeds != "" && (access.Closed != httpapi.PersonOnly || !slices.Contains(permissions, access.PersonNeeds)):
			t.Errorf("%s: PersonNeeds %q goes only on a personOnly row, with a real permission", key, access.PersonNeeds)
		}
		if !callable(access) {
			continue
		}
		tool, isCovered := covered[key]
		_, isPending := mcpToolPending[key]
		if !isCovered && !isPending {
			t.Errorf("%s is open to agent tokens (needs %q) but no MCP tool calls it: add a tool in server/internal/mcpserver/catalog*.go "+
				"that lists it in Spec.REST (or, only with a tracking issue, add it to mcpToolPending in %s)", key, access.Permission, pendingFile)
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
		case access.Closed == httpapi.PersonOnly:
			personOnly++
		case access.Closed != "" && access.Closed != httpapi.NotForPeople:
			closed++
		}
	}
	if closed > maxClosedToTokens {
		t.Errorf("%d routes in %s are closed to every token, over the ceiling of %d: say what the route needs instead",
			closed, routeAccessFile, maxClosedToTokens)
	}
	if personOnly > maxPersonOnly {
		t.Errorf("%d routes in %s are personOnly, over the ceiling of %d: a route a person can do is for agents too, with the permission it needs",
			personOnly, routeAccessFile, maxPersonOnly)
	}
	for rest, tool := range covered {
		if access := table[rest]; !callable(access) {
			t.Errorf("MCP tool %s calls %s, which is closed to every token in %s: drop it from Spec.REST", tool, rest, routeAccessFile)
		}
	}
}

// A tool states what it needs, and that is exactly what its routes need: a tool never claims less (a
// model would call it and be refused) or more (whoami would hide a tool the token can use).
func TestEveryToolNeedsWhatItsRoutesNeed(t *testing.T) {
	table := httpapi.RouteAccessTable()
	for _, spec := range mcpserver.Catalog() {
		if spec.Operator {
			continue
		}
		want := []string{}
		for _, rest := range spec.REST {
			access := table[rest]
			if access.Closed == httpapi.PersonOnly {
				t.Errorf("%s calls %s, which only a person may call: drop the route or the tool", spec.Name, rest)
			}
			if !access.Anyone && access.Permission != "" && !slices.Contains(want, access.Permission) {
				want = append(want, access.Permission)
			}
		}
		for _, need := range spec.BodyNeeds {
			if !slices.ContainsFunc(spec.REST, func(rest string) bool { return table[rest].ByHandler }) {
				t.Errorf("%s states BodyNeeds %s, but none of its routes decides by the body", spec.Name, need)
			}
			if !slices.Contains(want, need) {
				want = append(want, need)
			}
		}
		slices.Sort(want)
		if !slices.Equal(spec.Needs, want) {
			t.Errorf("%s needs %v, its routes need %v", spec.Name, spec.Needs, want)
		}
		if !strings.HasSuffix(spec.Description, "Needs: "+strings.Join(want, ", ")+".") && len(want) > 0 {
			t.Errorf("%s: its description does not end with Needs: %s.", spec.Name, strings.Join(want, ", "))
		}
	}
}
