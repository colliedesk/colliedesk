package mcpserver_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// webSource is the web app, relative to this package.
const webSource = "../../../web/src"

var (
	apiPathLiteral = regexp.MustCompile("[`'\"](/api/v1/[^`'\"?]*)")
	pathParameter  = regexp.MustCompile(`\$\{[^}]*\}|\{[^}]*\}`)
)

// A UI action is available to agents only if it goes through a route of Routes(), which the parity
// test above holds to a classification and an MCP tool. This test closes the loop on the web side:
// every API path the web app calls must be one of those routes.
func TestEveryAPIPathTheWebAppCallsIsARoute(t *testing.T) {
	routes := map[string]bool{}
	for _, route := range httpapi.Routes() {
		routes[normalizePath(route.Pattern)] = true
	}
	calls := map[string]string{}
	err := filepath.WalkDir(webSource, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")) ||
			strings.Contains(name, ".test.") || name == "generated.ts" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range apiPathLiteral.FindAllStringSubmatch(string(source), -1) {
			calls[normalizePath(match[1])] = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the web app in %s: %v", webSource, err)
	}
	if len(calls) == 0 {
		t.Fatalf("found no /api/v1 path in %s: the scan is broken", webSource)
	}
	for call, file := range calls {
		if !routes[call] {
			t.Errorf("%s calls %s, which is not a route in Routes(): add the route (contract first, then classify it for agent tokens) or fix the path", file, call)
		}
	}
}

// normalizePath turns `/a/${id}/b` and `/a/{id}/b` into `/a/{}/b`, and drops a query or suffix
// appended to the last segment (`/inbox/activity${since}`).
func normalizePath(path string) string {
	// A query built inline (`${since ? '?since=' : ''}`) holds quotes, so the literal ends inside it.
	if open := strings.LastIndex(path, "${"); open >= 0 && !strings.Contains(path[open:], "}") {
		path = path[:open]
	}
	path = pathParameter.ReplaceAllString(path, "{}")
	segments := strings.Split(strings.TrimSuffix(path, "/"), "/")
	last := segments[len(segments)-1]
	if last != "{}" && strings.HasSuffix(last, "{}") {
		segments[len(segments)-1] = strings.TrimSuffix(last, "{}")
	}
	return strings.Join(segments, "/")
}
