package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

func TestCatalogToolsMapOntoRealRoutesAndStateWhatTheyNeed(t *testing.T) {
	routes := map[string]bool{}
	for _, route := range httpapi.Routes() {
		routes[route.Method+" "+route.Pattern] = true
	}
	names := map[string]bool{}
	for _, spec := range mcpserver.Catalog() {
		if names[spec.Name] || !strings.HasPrefix(spec.Name, "colliedesk_") || strings.ContainsAny(spec.Name, ". -") {
			t.Errorf("tool name %q is duplicated or not underscore_case", spec.Name)
		}
		names[spec.Name] = true
		if want := "\n\nNeeds: "; !spec.Operator && !strings.Contains(spec.Description, want) {
			t.Errorf("%s: its description does not end with what it needs", spec.Name)
		}
		for _, need := range spec.Needs {
			if !slices.Contains(auth.AllPermissions(), need) {
				t.Errorf("%s needs %q, which is not a permission", spec.Name, need)
			}
		}
		for _, route := range spec.REST {
			method, pattern, _ := strings.Cut(route, " ")
			if !routes[route] || !strings.HasPrefix(pattern, "/api/v1/") || method == "" {
				t.Errorf("%s maps to %q, which is not a route of the REST API", spec.Name, route)
			}
		}
		if spec.Title == "" || spec.Description == "" || spec.Input == "" || spec.Output == "" {
			t.Errorf("%s: incomplete spec", spec.Name)
		}
	}
	if len(names) != len(toolNames)+len(operatorToolNames) {
		t.Errorf("catalog has %d tools, the list tests expect %d on %s and %d on %s", len(names), len(toolNames), mcpserver.Path, len(operatorToolNames), mcpserver.OperatorPath)
	}
}
