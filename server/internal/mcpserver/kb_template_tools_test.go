package mcpserver_test

import (
	"reflect"
	"strings"
	"testing"
)

// howToStart is how the How-to starter template begins, as the workspace gets it.
const howToStart = "Say in one sentence what the reader can do after these steps.\n\n## Before you start\n"

func templateNames(t *testing.T, list map[string]any) []string {
	t.Helper()
	var names []string
	for _, item := range list["templates"].([]any) {
		names = append(names, at(t, item, "team_text", "name").(string))
	}
	return names
}

func templateNamed(t *testing.T, list map[string]any, name string) map[string]any {
	t.Helper()
	for _, item := range list["templates"].([]any) {
		if at(t, item, "team_text", "name") == name {
			return item.(map[string]any)
		}
	}
	t.Fatalf("no template %q in %v", name, list)
	return nil
}

func TestAgentStartsArticlesFromKnowledgeBaseTemplates(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Template Desk", "admin@kb-templates.example.com")
	session := e.connect(w.send, "")

	starters := e.ok(session, "colliedesk_kb_templates_list", nil)
	if names := templateNames(t, starters); !reflect.DeepEqual(names, []string{"FAQ", "How-to", "Troubleshooting"}) {
		t.Fatalf("a new workspace's templates = %v, want the three starters by name", names)
	}
	howTo := templateNamed(t, starters, "How-to")
	if at(t, howTo, "team_text", "description") != "Steps to finish one task, with what to check at the end." ||
		!strings.HasPrefix(at(t, howTo, "team_text", "body").(string), howToStart) {
		t.Fatalf("How-to = %v", howTo)
	}

	collection := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"})
	collectionID := at(t, collection, "collection", "id").(string)
	fromHowTo := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Invite a teammate", "template_id": howTo["id"]})
	if body := at(t, fromHowTo, "article", "team_text", "body").(string); !strings.HasPrefix(body, howToStart) || !strings.Contains(body, "## Check that it worked") {
		t.Fatalf("article from How-to has body %q", body)
	}
	if message := e.fails(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Both", "body": "Text.", "template_id": howTo["id"]}); !strings.Contains(message, "body or template_id, not both") {
		t.Errorf("body and template_id together: %q", message)
	}

	created := e.ok(session, "colliedesk_kb_template_create", map[string]any{"name": "Release note", "description": "What changed.", "body": "## What changed\n"})
	releaseID := at(t, created, "template", "id").(string)
	if at(t, created, "template", "last_change", "via_token") != "test send" {
		t.Errorf("create = %v, want the token named in last_change", created)
	}
	edited := e.ok(session, "colliedesk_kb_template_update", map[string]any{"template_id": releaseID, "updated_at": at(t, created, "template", "updated_at"), "body": "## What changed\n\n## Who it affects\n"})

	withDefault := e.ok(session, "colliedesk_kb_collection_update", map[string]any{"collection_id": collectionID, "updated_at": at(t, collection, "collection", "updated_at"), "default_template_id": releaseID})
	if at(t, withDefault, "collection", "default_template_id") != releaseID {
		t.Fatalf("collection with a default template = %v", withDefault)
	}
	fromDefault := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "November release"})
	if body := at(t, fromDefault, "article", "team_text", "body"); body != "## What changed\n\n## Who it affects\n" {
		t.Fatalf("article without a body in a collection with a default template has body %q", body)
	}

	// Editing the template later never changes the article made from it.
	e.ok(session, "colliedesk_kb_template_update", map[string]any{"template_id": releaseID, "updated_at": at(t, edited, "template", "updated_at"), "body": "## Changed later\n"})
	reread := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": at(t, fromDefault, "article", "id")})
	if body := at(t, reread, "article", "team_text", "body"); body != "## What changed\n\n## Who it affects\n" {
		t.Fatalf("after the template changed, the article body = %q", body)
	}

	byName := e.ok(session, "colliedesk_kb_template_get", map[string]any{"name": "RELEASE NOTE"})
	deleted := e.ok(session, "colliedesk_kb_template_delete", map[string]any{"template_id": releaseID, "updated_at": at(t, byName, "template", "updated_at")})
	if at(t, deleted, "in_trash") != true || !strings.Contains(at(t, deleted, "next").(string), "colliedesk_kb_template_undelete") {
		t.Fatalf("delete = %v", deleted)
	}
	collections := e.ok(session, "colliedesk_kb_collections_list", nil)
	if at(t, collections, "collections", 0, "default_template_id") != nil {
		t.Errorf("trashing the default template must clear it: %v", collections)
	}
	refused := e.fails(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "From the trash", "template_id": releaseID})
	if !strings.Contains(refused, "template is in the trash; undelete it or pick another") || !strings.Contains(refused, "colliedesk_kb_template_undelete") {
		t.Errorf("article from a trashed template: %q", refused)
	}

	trash := e.ok(session, "colliedesk_kb_templates_list", map[string]any{"trash": true})
	inTrash := templateNamed(t, trash, "Release note")
	if at(t, inTrash, "purges_at") == "" || at(t, inTrash, "deleted_by") == nil {
		t.Fatalf("trash = %v", trash)
	}
	found := e.ok(session, "colliedesk_kb_template_get", map[string]any{"template_id": releaseID})
	if at(t, found, "in_trash") != true {
		t.Errorf("get of a trashed template = %v, want in_trash", found)
	}
	undeleted := e.ok(session, "colliedesk_kb_template_undelete", map[string]any{"template_id": releaseID, "updated_at": at(t, inTrash, "updated_at")})

	history := e.ok(session, "colliedesk_kb_template_history", map[string]any{"template_id": releaseID})
	var kinds []string
	for _, version := range history["versions"].([]any) {
		kinds = append(kinds, at(t, version, "kind").(string))
	}
	if !reflect.DeepEqual(kinds, []string{"undelete", "delete", "edit", "edit", "create"}) {
		t.Fatalf("history kinds = %v", kinds)
	}
	restored := e.ok(session, "colliedesk_kb_template_version_restore", map[string]any{"template_id": releaseID, "version": 1, "updated_at": at(t, undeleted, "template", "updated_at")})
	if at(t, restored, "template", "team_text", "body") != "## What changed\n" || at(t, restored, "template", "last_change", "kind") != "restore" {
		t.Fatalf("restore version 1 = %v", restored)
	}
}

func TestReadOnlyTokenListsTemplatesButCannotWriteOrStartFromOne(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Template Read Desk", "admin@kb-template-read.example.com")
	writer := e.connect(w.send, "")
	collectionID := at(t, e.ok(writer, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id")

	reader := e.connect(w.read, "")
	faq := templateNamed(t, e.ok(reader, "colliedesk_kb_templates_list", nil), "FAQ")
	refusedWithoutPermission(t, e, w.read, "colliedesk_kb_template_create", map[string]any{"name": "Mine", "body": "## Text\n"})
	if message := refusedWithoutPermission(t, e, w.read, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "From FAQ", "template_id": faq["id"]}); !strings.Contains(message, "kb_articles:create") {
		t.Errorf("article from a template without kb_articles:create: %q", message)
	}
}

func TestTemplateOfAnotherWorkspaceIsNotFound(t *testing.T) {
	e := newEnv(t)
	mine := e.newWorkspace("KB Template Mine", "admin@kb-template-mine.example.com")
	theirs := e.newWorkspace("KB Template Theirs", "admin@kb-template-theirs.example.com")
	theirFAQ := templateNamed(t, e.ok(e.connect(theirs.send, ""), "colliedesk_kb_templates_list", nil), "FAQ")

	session := e.connect(mine.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id")
	for tool, args := range map[string]map[string]any{
		"colliedesk_kb_template_get":    {"template_id": theirFAQ["id"]},
		"colliedesk_kb_template_update": {"template_id": theirFAQ["id"], "updated_at": theirFAQ["updated_at"], "body": "## Mine now\n"},
		"colliedesk_kb_article_create":  {"collection_id": collectionID, "title": "Theirs", "template_id": theirFAQ["id"]},
	} {
		if message := e.fails(session, tool, args); !strings.Contains(message, "No template") || !strings.Contains(message, "colliedesk_kb_templates_list") {
			t.Errorf("%s with another workspace's template: %q", tool, message)
		}
	}
}
