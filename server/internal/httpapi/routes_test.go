package httpapi_test

import (
	"testing"

	"github.com/hclabsco/colliedesk/server/internal/httpapi"
)

func TestEveryAPIRouteDeclaresPermission(t *testing.T) {
	known := map[string]bool{
		"public": true, "authenticated": true, "conversations:read": true, "conversations:write": true,
		"contacts:read": true, "contacts:write": true, "access:read": true, "access:write": true,
	}
	for _, route := range httpapi.Routes() {
		if !known[route.Permission] {
			t.Errorf("%s %s has unknown permission %q", route.Method, route.Pattern, route.Permission)
		}
	}
}
