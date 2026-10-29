package httpapi_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// Every route a channel type serves below the help center is in the contract, tagged HelpCenterChat
// with the help center as its server and no token, and listed with why no token tier classifies it;
// nothing else carries the tag.
func TestEveryHelpCenterRouteIsListedAndInTheContract(t *testing.T) {
	api, _ := testAPI(t)
	registered := api.(*httpapi.Server).HelpCenterRoutes()
	listed := httpapi.HelpCenterPublicRoutes()
	document, err := contract()
	if err != nil {
		t.Fatal(err)
	}
	var inContract []string
	for path, item := range document.Paths.Map() {
		for method, operation := range item.Operations() {
			if !slices.Contains(operation.Tags, "HelpCenterChat") {
				continue
			}
			key := method + " " + path
			inContract = append(inContract, key)
			if operation.Extensions["x-colliedesk-surface"] != "help-center" || operation.Security == nil || len(*operation.Security) != 0 || len(item.Servers) == 0 {
				t.Errorf("%s: a help center route has the help center as its server, x-colliedesk-surface: help-center and security: []", key)
			}
			if strings.HasPrefix(path, "/api/") {
				t.Errorf("%s: a help center route is never under /api", key)
			}
		}
	}
	slices.Sort(registered)
	slices.Sort(inContract)
	if !slices.Equal(registered, inContract) {
		t.Errorf("help center routes %v, contract's HelpCenterChat paths %v", registered, inContract)
	}
	for _, key := range registered {
		if listed[key] == "" {
			t.Errorf("%s is not in helpCenterPublicRoutes (token-scope.go)", key)
		}
		delete(listed, key)
	}
	for key := range listed {
		t.Errorf("helpCenterPublicRoutes lists %s, which no type serves", key)
	}
}
