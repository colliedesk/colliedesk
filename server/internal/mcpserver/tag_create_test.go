package mcpserver_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// workspaceTagNames lists the tags of the workspace straight from the REST API, sorted.
func workspaceTagNames(t *testing.T, e *env, token string) []string {
	t.Helper()
	status, listed := e.rest(http.MethodGet, "/api/v1/tags", nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET tags status=%d", status)
	}
	names := []string{}
	for _, item := range listed["items"].([]any) {
		names = append(names, at(t, item, "name").(string))
	}
	slices.Sort(names)
	return names
}

func TestConversationUpdateCreatesAMissingTagAndAppliesIt(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	draft := e.connect(f.w.draft, "")
	args := map[string]any{"conversation_id": f.c3, "add_tags": []string{"  Refund ", "billing", "refund"}}

	out := e.ok(draft, "colliedesk_conversation_update", args)
	tags := at(t, out, "conversation", "tags").([]any)
	if len(tags) != 2 || !slices.Contains(tags, any("Refund")) || !slices.Contains(tags, any("billing")) {
		t.Errorf("result tags = %v, want Refund and billing", tags)
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"Refund", "billing"}) {
		t.Fatalf("workspace tags = %v, want billing and one Refund (trimmed, the spelling given first, no case duplicate)", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c3)); !slices.Equal(got, []string{"Refund", "billing"}) {
		t.Fatalf("REST shows tags %v on the conversation, want Refund and billing", got)
	}

	e.ok(draft, "colliedesk_conversation_update", args)
	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c2, "add_tags": []string{"REFUND"}})
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"Refund", "billing"}) {
		t.Errorf("repeating, or another spelling, created a tag: %v", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c2)); !slices.Equal(got, []string{"Refund"}) {
		t.Errorf("the other conversation has tags %v, want the existing Refund", got)
	}
}

func TestConversationUpdateCreatesNoTagWhenTheCallIsRefusedOrInvalid(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	cases := []struct {
		name  string
		token string
		args  map[string]any
		want  string
	}{
		{"unknown assignee", f.w.draft, map[string]any{"conversation_id": f.c3, "add_tags": []string{"brand-new"}, "assignee": "nobody@inbox.example.com"}, "No team member"},
		{"unknown tag to remove", f.w.draft, map[string]any{"conversation_id": f.c3, "add_tags": []string{"brand-new"}, "remove_tags": []string{"nonesuch"}}, "No tag named"},
		{"same tag added and removed", f.w.draft, map[string]any{"conversation_id": f.c3, "add_tags": []string{"brand-new", "billing"}, "remove_tags": []string{"Billing"}}, "both add_tags and remove_tags"},
		{"blank name", f.w.draft, map[string]any{"conversation_id": f.c3, "add_tags": []string{"   "}}, "empty"},
		{"unknown conversation", f.w.draft, map[string]any{"conversation_id": "00000000-0000-4000-8000-000000000000", "add_tags": []string{"brand-new"}}, "No conversation"},
	}
	// Below the tier the first change is filed as an agent request; no tag is created until a person approves.
	requestedForTier(t, e, f.w.read, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "add_tags": []string{"brand-new"}})
	for _, c := range cases {
		if message := e.fails(e.connect(c.token, ""), "colliedesk_conversation_update", c.args); !strings.Contains(message, c.want) {
			t.Errorf("%s: message %q lacks %q", c.name, message, c.want)
		}
	}
	if got := workspaceTagNames(t, e, f.w.admin); !slices.Equal(got, []string{"billing"}) {
		t.Errorf("refused calls left tags behind: %v", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c3)); len(got) != 0 {
		t.Errorf("refused calls tagged the conversation: %v", got)
	}
}

// The name another workspace uses is no reason to touch its tag: a missing tag is created here.
func TestMissingTagIsCreatedInTheCallersWorkspaceOnly(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	foreign := e.created("/api/v1/tags", map[string]any{"name": "shared-name"}, f.other.admin)

	e.ok(e.connect(f.w.draft, ""), "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "add_tags": []string{"shared-name"}})

	status, listed := e.rest(http.MethodGet, "/api/v1/tags", nil, f.w.admin)
	if status != http.StatusOK || len(listed["items"].([]any)) != 2 {
		t.Fatalf("workspace A tags: %d %v, want billing and its own shared-name", status, listed)
	}
	for _, item := range listed["items"].([]any) {
		if at(t, item, "id") == foreign {
			t.Errorf("workspace A uses the tag id of workspace B: %v", listed)
		}
	}
	if got := workspaceTagNames(t, e, f.other.admin); !slices.Equal(got, []string{"shared-name"}) {
		t.Errorf("workspace B tags = %v, want its own single tag", got)
	}
	if got := tagNames(t, restConversation(t, e, f.other, f.otherConvID)); len(got) != 0 {
		t.Errorf("workspace B conversation was tagged: %v", got)
	}
}
