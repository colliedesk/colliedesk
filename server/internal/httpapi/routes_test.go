package httpapi_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestEveryAPIRouteDeclaresPermission(t *testing.T) {
	// Route.Permission says only how a route is reached; what a caller needs is in routeAccess.
	known := map[string]bool{"public": true, "session": true, "channels:inbound": true, "operator": true}
	for _, route := range httpapi.Routes() {
		if !known[route.Permission] {
			t.Errorf("%s %s has unknown permission %q", route.Method, route.Pattern, route.Permission)
		}
	}
}
