package mcpserver_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// An agent sets the custom domain, reads the records to set, checks it against a stubbed DNS (the
// external boundary), and removes it; a token below admin files a request instead.
func TestHelpCenterDomainToolsSetCheckAndRemove(t *testing.T) {
	var mu sync.Mutex
	txt := map[string][]string{}
	lookupTXT := func(_ context.Context, name string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if values, ok := txt[name]; ok {
			return values, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	lookupCNAME := func(_ context.Context, host string) (string, error) {
		return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	e := newEnvBuilt(t, func(config *httpapi.Config) { config.LookupTXT, config.LookupCNAME = lookupTXT, lookupCNAME }, nil)
	w := e.newWorkspace("Domain Tools Desk", "admin@domain-tools.example.com")

	read := e.connect(w.read, "")
	if got := e.ok(read, "colliedesk_help_center_domain_get", nil); at(t, got, "domain", "status") != "none" {
		t.Fatalf("get = %v", got)
	}
	send := e.connect(w.send, "")
	if proposed := e.ok(send, "colliedesk_help_center_domain_set", map[string]any{"hostname": "help.tools.example"}); proposed["agent_request"] == nil {
		t.Fatalf("send token set = %v, want an agent request", proposed)
	}
	admin := e.connect(w.adminTier, "")
	if message := e.fails(admin, "colliedesk_help_center_domain_set", map[string]any{"hostname": "192.0.2.1"}); !strings.Contains(message, "IP address") {
		t.Fatalf("an IP address: %q", message)
	}
	set := e.ok(admin, "colliedesk_help_center_domain_set", map[string]any{"hostname": "help.tools.example"})
	records, _ := at(t, set, "domain", "records").([]any)
	if at(t, set, "domain", "status") != "pending" || len(records) != 2 {
		t.Fatalf("set = %v", set)
	}
	token := records[1].(map[string]any)["value"].(string)
	if checked := e.ok(admin, "colliedesk_help_center_domain_check", nil); at(t, checked, "domain", "status") != "pending" || at(t, checked, "domain", "check_error") == nil {
		t.Fatalf("check without DNS = %v", checked)
	}
	mu.Lock()
	txt["_help-center-verify.help.tools.example"] = []string{token}
	mu.Unlock()
	if checked := e.ok(admin, "colliedesk_help_center_domain_check", nil); at(t, checked, "domain", "status") != "active" {
		t.Fatalf("check with the TXT token = %v", checked)
	}
	if removed := e.ok(admin, "colliedesk_help_center_domain_remove", nil); at(t, removed, "domain", "status") != "none" {
		t.Fatalf("remove = %v", removed)
	}
	if got := e.ok(read, "colliedesk_help_center_domain_get", nil); at(t, got, "domain", "status") != "none" {
		t.Fatalf("get after remove = %v", got)
	}
}
