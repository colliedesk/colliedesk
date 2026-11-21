package mcpserver_test

import (
	"testing"
)

// An agent sets who can see an article or a collection, reads the effective visibility and the
// collection that limits it, and lists the knowledge base by visibility; a read-only token cannot
// change visibility.
func TestAgentsSetAndListKnowledgeBaseVisibility(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Visibility Tools", "admin@kb-visibility-tools.example.com")
	session := e.connect(w.send, "")

	team := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Team notes", "visibility": "agents"})
	teamID := at(t, team, "collection", "id").(string)
	if at(t, team, "collection", "visibility") != "agents" || at(t, team, "collection", "effective_visibility") != "agents" {
		t.Fatalf("collection create = %v", team)
	}
	guides := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"})
	guidesID := at(t, guides, "collection", "id").(string)
	if at(t, guides, "collection", "visibility") != "everyone" {
		t.Fatalf("a collection without visibility = %v, want everyone", guides)
	}

	open := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": guidesID, "title": "Change your password", "body": "Anyone.", "status": "published"})
	quoted := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": guidesID, "title": "Refund rules", "body": "Help chat may quote this.",
		"status": "published", "visibility": "help_chat"})
	inTeam := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": teamID, "title": "Escalations", "body": "Call the lead.", "status": "published"})
	if at(t, quoted, "article", "visibility") != "help_chat" || at(t, quoted, "article", "effective_visibility") != "help_chat" {
		t.Fatalf("article create with help_chat = %v", quoted)
	}
	if at(t, inTeam, "article", "visibility") != "everyone" || at(t, inTeam, "article", "effective_visibility") != "agents" ||
		at(t, inTeam, "article", "visibility_from", "name") != "Team notes" {
		t.Fatalf("an article in an agents-only collection = %v", inTeam)
	}
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_create", inTeam)

	listed := func(visibility string) []string {
		var titles []string
		for _, row := range e.ok(session, "colliedesk_kb_articles_search", map[string]any{"visibility": visibility})["articles"].([]any) {
			titles = append(titles, at(t, row, "team_text", "title").(string))
		}
		return titles
	}
	if got := listed("agents"); len(got) != 1 || got[0] != "Escalations" {
		t.Fatalf("agents-only articles = %v", got)
	}
	if got := listed("help_chat"); len(got) != 1 || got[0] != "Refund rules" {
		t.Fatalf("help-chat-only articles = %v", got)
	}

	lowered := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, open, "article", "id"), "updated_at": at(t, open, "article", "updated_at"),
		"visibility": "agents"})
	if at(t, lowered, "article", "visibility") != "agents" || at(t, lowered, "article", "effective_visibility") != "agents" {
		t.Fatalf("article update to agents = %v", lowered)
	}
	raised := e.ok(session, "colliedesk_kb_collection_update", map[string]any{"collection_id": teamID, "updated_at": at(t, team, "collection", "updated_at"), "visibility": "everyone"})
	if at(t, raised, "collection", "visibility") != "everyone" {
		t.Fatalf("collection update to everyone = %v", raised)
	}
	e.requireMatchesOutputSchema(session, "colliedesk_kb_collection_update", raised)
	if got := listed("everyone"); len(got) != 1 || got[0] != "Escalations" {
		t.Fatalf("articles for everyone after the changes = %v", got)
	}

	read := e.connect(w.read, "")
	e.refused(read, "colliedesk_kb_collection_update", map[string]any{"collection_id": guidesID, "updated_at": at(t, guides, "collection", "updated_at"), "visibility": "agents"})
	e.refused(read, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, quoted, "article", "id"), "updated_at": at(t, quoted, "article", "updated_at"), "visibility": "everyone"})
	if got := listed("agents"); len(got) != 1 || got[0] != "Change your password" {
		t.Fatalf("a refused call changed visibility: agents-only articles = %v", got)
	}
}
