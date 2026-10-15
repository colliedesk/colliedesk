package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

func TestCatalogToolsMapOntoRealRoutesAndStateTheirTier(t *testing.T) {
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
		if !slices.Contains([]string{"read", "draft", "send", "any"}, spec.Tier) {
			t.Errorf("%s: tier %q", spec.Name, spec.Tier)
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
	if len(names) != len(toolNames) {
		t.Errorf("catalog has %d tools, the list test expects %d", len(names), len(toolNames))
	}
}
