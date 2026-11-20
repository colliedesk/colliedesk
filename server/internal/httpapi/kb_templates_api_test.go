package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbTemplateView struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Body        string           `json:"body"`
	UpdatedAt   string           `json:"updated_at"`
	LastChange  *replyChangeView `json:"last_change"`
}

type kbTemplateVersionView struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Body    string `json:"body"`
}

func kbTemplate(t *testing.T, api http.Handler, token string, body map[string]any) kbTemplateView {
	t.Helper()
	var template kbTemplateView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/templates", body, token, http.StatusCreated, &template)
	return template
}

func kbTemplateNames(t *testing.T, api http.Handler, token, path string) []string {
	t.Helper()
	var page struct {
		Items []kbTemplateView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, path, nil, token, http.StatusOK, &page)
	names := []string{}
	for _, item := range page.Items {
		names = append(names, item.Name)
	}
	return names
}

func kbTemplateByName(t *testing.T, api http.Handler, token, name string) kbTemplateView {
	t.Helper()
	var page struct {
		Items []kbTemplateView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/templates", nil, token, http.StatusOK, &page)
	for _, item := range page.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no template named %q in %+v", name, page.Items)
	return kbTemplateView{}
}

func kbTemplateVersionKinds(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var page struct {
		Items []kbTemplateVersionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/templates/"+id+"/versions", nil, token, http.StatusOK, &page)
	kinds := []string{}
	for _, item := range page.Items {
		kinds = append(kinds, item.Kind)
	}
	return strings.Join(kinds, ",")
}

func deleteKBTemplate(t *testing.T, api http.Handler, token string, template kbTemplateView) {
	t.Helper()
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/templates/"+template.ID+"?updated_at="+url.QueryEscape(template.UpdatedAt), nil, token, http.StatusNoContent, nil)
}

func TestANewWorkspaceStartsWithThreeStarterTemplatesAndADeletedOneNeverComesBack(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "KB Starters", "admin@kb-starters.example.com")
	workspace := workspaceID(t, api, admin)

	if got := strings.Join(kbTemplateNames(t, api, admin, "/api/v1/kb/templates"), "|"); got != "FAQ|How-to|Troubleshooting" {
		t.Fatalf("starter templates = %s, want FAQ|How-to|Troubleshooting", got)
	}
	howTo := kbTemplateByName(t, api, admin, "How-to")
	if !strings.Contains(howTo.Body, "## Steps") || howTo.Description == "" || kbTemplateVersionKinds(t, api, admin, howTo.ID) != "create" {
		t.Fatalf("How-to = %+v", howTo)
	}

	deleteKBTemplate(t, api, admin, howTo)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `SELECT kb_seed_starter_templates($1)`, workspace); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(kbTemplateNames(t, api, admin, "/api/v1/kb/templates"), "|"); got != "FAQ|Troubleshooting" {
		t.Fatalf("after a delete and a second seed = %s, want FAQ|Troubleshooting", got)
	}
	// Purged for good, it still does not come back.
	if _, err := pool.Exec(ctx, `UPDATE kb_templates SET deleted_at=now()-interval '31 days' WHERE id=$1`, howTo.ID); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).PurgeKBTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if purged.Templates != 1 {
		t.Fatalf("purged = %+v, want one template", purged)
	}
	if _, err := pool.Exec(ctx, `SELECT kb_seed_starter_templates($1)`, workspace); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(kbTemplateNames(t, api, admin, "/api/v1/kb/templates"), "|"); got != "FAQ|Troubleshooting" {
		t.Fatalf("after a purge and a third seed = %s, want FAQ|Troubleshooting", got)
	}
}

func TestTemplatesAreEditedTrashedUndeletedAndRestoredWithTheirHistory(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Template Desk", "admin@kb-template-desk.example.com")
	release := kbTemplate(t, api, admin, map[string]any{"name": "Release note", "description": "What changed and why.", "body": "## What changed\n"})
	if release.Description != "What changed and why." || release.LastChange == nil || release.LastChange.Kind != "create" {
		t.Fatalf("created = %+v", release)
	}
	duplicate := request(t, api, http.MethodPost, "/api/v1/kb/templates", map[string]any{"name": "release NOTE", "body": "x"}, admin, nil)
	if duplicate.Code != http.StatusConflict || errorCode(t, duplicate) != "duplicate_name" {
		t.Fatalf("same name status=%d body=%s, want 409 duplicate_name", duplicate.Code, duplicate.Body.String())
	}
	for _, bad := range []map[string]any{{"name": "", "body": "x"}, {"name": "Empty", "body": "  "}, {"name": "Two lines", "description": "one\ntwo", "body": "x"}} {
		if response := request(t, api, http.MethodPost, "/api/v1/kb/templates", bad, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("create %v status=%d body=%s, want 400", bad, response.Code, response.Body.String())
		}
	}

	var edited kbTemplateView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/templates/"+release.ID, map[string]any{"updated_at": release.UpdatedAt, "body": "## What changed\n\n## Why\n"}, admin, http.StatusOK, &edited)
	stale := request(t, api, http.MethodPatch, "/api/v1/kb/templates/"+release.ID, map[string]any{"updated_at": release.UpdatedAt, "body": "lost"}, admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("stale edit status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	var got kbTemplateView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/templates/"+release.ID, nil, admin, http.StatusOK, &got)
	if got.Body != "## What changed\n\n## Why\n" {
		t.Fatalf("read back = %+v", got)
	}

	deleteKBTemplate(t, api, admin, edited)
	if names := kbTemplateNames(t, api, admin, "/api/v1/kb/templates/trash"); len(names) != 1 || names[0] != "Release note" {
		t.Fatalf("trash = %v", names)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/kb/templates/"+release.ID, nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("trashed template read status=%d, want 404", response.Code)
	}
	var trash struct {
		Items []kbTemplateView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/templates/trash", nil, admin, http.StatusOK, &trash)
	// A repeated press (same Idempotency-Key) answers the first undelete and restore again and adds no version.
	var back, backAgain kbTemplateView
	for _, target := range []*kbTemplateView{&back, &backAgain} {
		response := postWithKey(t, api, admin, "/api/v1/kb/templates/"+release.ID+"/restore", "undelete-release", map[string]any{"updated_at": trash.Items[0].UpdatedAt})
		if response.Code != http.StatusOK {
			t.Fatalf("undelete status=%d body=%s, want 200", response.Code, response.Body.String())
		}
		decode(t, response, target)
	}
	var restored, restoredAgain kbTemplateView
	for _, target := range []*kbTemplateView{&restored, &restoredAgain} {
		response := postWithKey(t, api, admin, "/api/v1/kb/templates/"+release.ID+"/versions/1/restore", "restore-release-1", map[string]any{"updated_at": back.UpdatedAt})
		if response.Code != http.StatusOK {
			t.Fatalf("restore version 1 status=%d body=%s, want 200", response.Code, response.Body.String())
		}
		decode(t, response, target)
	}
	if backAgain.UpdatedAt != back.UpdatedAt || restoredAgain.UpdatedAt != restored.UpdatedAt {
		t.Fatalf("repeated presses answered other changes: %+v %+v / %+v %+v", back, backAgain, restored, restoredAgain)
	}
	if restored.Body != "## What changed\n" {
		t.Fatalf("version 1 restored = %+v", restored)
	}
	if kinds := kbTemplateVersionKinds(t, api, admin, release.ID); kinds != "restore,undelete,delete,edit,create" {
		t.Fatalf("versions = %s", kinds)
	}
}

func TestTemplatesNeverCrossWorkspaces(t *testing.T) {
	api, service := testAPI(t)
	alpha := createSession(t, api, service, "KB Template Alpha", "admin@kb-template-alpha.example.com")
	beta := createSession(t, api, service, "KB Template Beta", "admin@kb-template-beta.example.com")
	secret := kbTemplate(t, api, alpha, map[string]any{"name": "Alpha only", "body": "alpha-only template text"})
	betaCollection := kbCollection(t, api, beta, map[string]any{"name": "Beta docs"})
	at := url.QueryEscape(secret.UpdatedAt)
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/kb/templates/" + secret.ID, nil},
		{http.MethodGet, "/api/v1/kb/templates/" + secret.ID + "/versions", nil},
		{http.MethodPatch, "/api/v1/kb/templates/" + secret.ID, map[string]any{"updated_at": secret.UpdatedAt, "name": "Taken"}},
		{http.MethodDelete, "/api/v1/kb/templates/" + secret.ID + "?updated_at=" + at, nil},
		{http.MethodPost, "/api/v1/kb/templates/" + secret.ID + "/restore", map[string]any{"updated_at": secret.UpdatedAt}},
		{http.MethodPost, "/api/v1/kb/templates/" + secret.ID + "/versions/1/restore", map[string]any{"updated_at": secret.UpdatedAt}},
		{http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": betaCollection.ID, "title": "Smuggled", "template_id": secret.ID}},
		{http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Smuggled", "default_template_id": secret.ID}},
		{http.MethodPatch, "/api/v1/kb/collections/" + betaCollection.ID, map[string]any{"updated_at": betaCollection.UpdatedAt, "default_template_id": secret.ID}},
	} {
		if response := request(t, api, call.method, call.path, call.body, beta, nil); response.Code != http.StatusNotFound {
			t.Errorf("beta %s %s status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/kb/templates", "/api/v1/kb/templates/trash"} {
		for _, name := range kbTemplateNames(t, api, beta, path) {
			if name == "Alpha only" {
				t.Errorf("beta %s lists alpha's template", path)
			}
		}
	}
}

func TestTemplatesNeedTheKnowledgeBasePermissions(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Template Tokens", "admin@kb-template-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	if names := kbTemplateNames(t, api, read, "/api/v1/kb/templates"); len(names) != 3 {
		t.Fatalf("read token lists %v, want the three starters", names)
	}
	refused(t, api, http.MethodPost, "/api/v1/kb/templates", map[string]any{"name": "Agent", "body": "x"}, read, "kb_articles:update")
	faq := kbTemplateByName(t, api, read, "FAQ")
	refused(t, api, http.MethodPatch, "/api/v1/kb/templates/"+faq.ID, map[string]any{"updated_at": faq.UpdatedAt, "body": "x"}, read, "kb_articles:update")
	refused(t, api, http.MethodDelete, "/api/v1/kb/templates/"+faq.ID+"?updated_at="+url.QueryEscape(faq.UpdatedAt), nil, read, "kb_articles:update")
}
