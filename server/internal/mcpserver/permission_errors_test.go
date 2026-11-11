package mcpserver

import (
	"strings"
	"testing"
)

// A refusal for a missing permission reaches the model with Colliedesk's own sentence, the permissions
// by name, and what to do: tell the person, never try again with the same token.
func TestAPermissionMissingErrorReachesTheModelWithTheFix(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		failure restError
		want    []string
	}{
		{"token lacks it", "colliedesk_workspace_export", restError{Status: 403, Code: "permission_missing", Message: "This agent token cannot export workspace data (workspace:export).",
			MissingPermissions: []string{"workspace:export"}, MissingFrom: "token"},
			[]string{"cannot export workspace data", "Missing: workspace:export.", "Settings → External AI agents", "Do not try the call again"}},
		{"person lacks it", "colliedesk_role_create", restError{Status: 403, Code: "permission_missing", Message: "Your roles do not allow changing roles (team:manage_roles).",
			MissingPermissions: []string{"team:manage_roles"}, MissingFrom: "person", Ask: []string{"Chinh Pham", "Long Bui"}},
			[]string{"Missing: team:manage_roles.", "Chinh Pham, Long Bui can give them a role"}},
		{"reply without send", "colliedesk_reply_send", restError{Status: 403, Code: "permission_missing", Message: "This agent token cannot send replies to customers (messages:send).",
			MissingPermissions: []string{"messages:send"}, MissingFrom: "token"},
			[]string{"Missing: messages:send.", "Do not try the call again"}},
		{"role beyond yours", "colliedesk_member_roles_set", restError{Status: 403, Code: "role_exceeds_yours", Message: "You cannot give Support lead: it holds permissions you lack.",
			MissingPermissions: []string{"billing:manage"}},
			[]string{"You cannot give Support lead", "Missing: billing:manage."}},
		{"person only", "colliedesk_whoami", restError{Status: 403, Code: "person_only", Message: "Only a person creates agent tokens.", Where: "/settings#agents-heading"},
			[]string{"Only a person creates agent tokens.", "/settings#agents-heading", "No agent token can do it"}},
	}
	for _, c := range cases {
		failure := c.failure
		got := (&call{spec: Spec{Name: c.tool}}).refuse(&failure, "")
		message := ""
		if toolErr, ok := got.(*toolError); ok {
			message = toolErr.message
		} else {
			t.Errorf("%s: %v is not a message for the model", c.name, got)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(message, want) {
				t.Errorf("%s: %q lacks %q", c.name, message, want)
			}
		}
	}
}
