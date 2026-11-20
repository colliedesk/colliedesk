package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbCollectionWithTemplate struct {
	kbCollectionView
	DefaultTemplateID *string `json:"default_template_id"`
}

func TestAnArticleCreatedFromATemplateCopiesItsBodyOnce(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB From Template", "admin@kb-from-template.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	template := kbTemplate(t, api, admin, map[string]any{"name": "Setup guide", "body": "## Before you start\n\n## Steps\n"})

	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Connect your mailbox", "template_id": template.ID})
	if article.BodyMD != "## Before you start\n\n## Steps\n" || article.Status != "draft" {
		t.Fatalf("article from a template = %+v", article)
	}
	// Editing the template later never changes the article made from it.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/templates/"+template.ID, map[string]any{"updated_at": template.UpdatedAt, "body": "## Rewritten\n"}, admin, http.StatusOK, nil)
	var again kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &again)
	if again.BodyMD != "## Before you start\n\n## Steps\n" {
		t.Fatalf("the article followed the template: %q", again.BodyMD)
	}

	both := request(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Both", "template_id": template.ID, "body_md": "x"}, admin, nil)
	if both.Code != http.StatusBadRequest {
		t.Fatalf("template_id with body_md status=%d body=%s, want 400", both.Code, both.Body.String())
	}
	current := kbTemplateByName(t, api, admin, "Setup guide")
	deleteKBTemplate(t, api, admin, current)
	trashed := request(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Late", "template_id": template.ID}, admin, nil)
	if trashed.Code != http.StatusConflict || !strings.Contains(trashed.Body.String(), "template is in the trash; undelete it or pick another") {
		t.Fatalf("trashed template status=%d body=%s, want 409 naming the trash", trashed.Code, trashed.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/kb/articles", nil, admin, nil), trashed)
}

func TestACollectionDefaultTemplateIsClearedWhenTheTemplateIsTrashed(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Default Template", "admin@kb-default-template.example.com")
	faq := kbTemplateByName(t, api, admin, "FAQ")
	var collection kbCollectionWithTemplate
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Questions", "default_template_id": faq.ID}, admin, http.StatusCreated, &collection)
	if collection.DefaultTemplateID == nil || *collection.DefaultTemplateID != faq.ID {
		t.Fatalf("created collection = %+v", collection)
	}
	var cleared kbCollectionWithTemplate
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "default_template_id": nil}, admin, http.StatusOK, &cleared)
	if cleared.DefaultTemplateID != nil {
		t.Fatalf("null did not clear the default: %+v", cleared)
	}
	var set kbCollectionWithTemplate
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": cleared.UpdatedAt, "default_template_id": faq.ID}, admin, http.StatusOK, &set)

	deleteKBTemplate(t, api, admin, faq)
	var page struct {
		Items []kbCollectionWithTemplate `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, admin, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].DefaultTemplateID != nil {
		t.Fatalf("collections after the template was trashed = %+v", page.Items)
	}
	refusedDefault := request(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": page.Items[0].UpdatedAt, "default_template_id": faq.ID}, admin, nil)
	if refusedDefault.Code != http.StatusConflict || !strings.Contains(refusedDefault.Body.String(), "template is in the trash") {
		t.Fatalf("a trashed default status=%d body=%s, want 409", refusedDefault.Code, refusedDefault.Body.String())
	}
}

// No public reader reads a template: the help center pages, its search, sitemap.xml, llms.txt, the
// Markdown copy, the images it serves and the help chat's retrieval. A published article's word is
// the control that each surface was really read.
func TestNoPublicSurfaceShowsATemplate(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Private Templates", "admin@kb-private-templates.example.com")
	workspace := workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Private Templates", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Quillfeather setup", "status": "published", "body_md": "The quillfeather answer."})
	image := uploadKBImage(t, api, admin, otherPNG(7), http.StatusCreated)
	kbTemplate(t, api, admin, map[string]any{"name": "Zephyrwick", "description": "Brambleton line", "body": "Glimmerstone steps ![s](" + image.URL + ")"})

	page := fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug)
	shown := map[string]bool{}
	for _, path := range []string{base, base + "/c/" + collection.Slug, page, page + ".md", base + "/search?q=quillfeather", base + "/search?q=glimmerstone",
		base + "/search?q=zephyrwick", base + "/sitemap.xml", base + "/llms.txt"} {
		response := visit(site, path, "198.51.100.61")
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
		body := strings.ToLower(response.Body.String())
		shown[path] = strings.Contains(body, "quillfeather")
		// The search page repeats what was asked; it must find nothing, and only the echo may name the word.
		if _, asked, ok := strings.Cut(path, "/search?q="); ok && asked != "quillfeather" {
			if !strings.Contains(body, "no results for “"+asked+"”") {
				t.Errorf("%s finds a template: %s", path, body)
			}
			body = strings.ReplaceAll(body, asked, "")
		}
		for _, word := range []string{"zephyrwick", "glimmerstone", "brambleton"} {
			if strings.Contains(body, word) {
				t.Errorf("%s shows the template word %q", path, word)
			}
		}
	}
	for _, path := range []string{page, page + ".md", base + "/search?q=quillfeather", base + "/llms.txt"} {
		if !shown[path] {
			t.Errorf("control: %s does not show the published article", path)
		}
	}
	if response := visit(site, base+"/images/"+image.ID, "198.51.100.62"); response.Code != http.StatusNotFound {
		t.Errorf("an image only a template names status=%d, want 404", response.Code)
	}
	buildAIRetrieval(t, pool)
	reader := ""
	docs := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "glimmerstone quillfeather", KBLimit: 5, KBReader: &reader})
	if len(docs) == 0 {
		t.Fatal("control: the help chat's retrieval found no article")
	}
	for _, doc := range docs {
		if strings.Contains(strings.ToLower(doc.Text+doc.Title), "glimmerstone") {
			t.Errorf("the help chat reads the template: %+v", doc)
		}
	}
}

func TestExportCarriesTheTemplatesOfItsWorkspaceOnly(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Template Export", "admin@kb-template-export.example.com")
	rival := createSession(t, api, service, "KB Template Rival", "admin@kb-template-rival.example.com")
	kept := kbTemplate(t, api, admin, map[string]any{"name": "Exported template", "body": "exported text"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/templates/"+kept.ID, map[string]any{"updated_at": kept.UpdatedAt, "body": "exported text, edited"}, admin, http.StatusOK, nil)
	kbTemplate(t, api, rival, map[string]any{"name": "rival-only template", "body": "rival-only template text"})

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	entries := unzip(t, response.Body.Bytes())
	templates := ndjsonLines(t, entries["kb-templates.ndjson"])
	// The three starters, and the one this workspace wrote.
	if len(templates) != 4 {
		t.Fatalf("kb-templates.ndjson = %v, want 4 lines", templates)
	}
	if versions := ndjsonLines(t, entries["kb-template-versions.ndjson"]); len(versions) != 5 {
		t.Fatalf("kb-template-versions.ndjson has %d lines, want 5", len(versions))
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_templates"] != 4 || manifest.Counts["kb_template_versions"] != 5 {
		t.Fatalf("manifest counts %v", manifest.Counts)
	}
	var all bytes.Buffer
	for name, content := range entries {
		all.WriteString(name + "\n")
		all.Write(content)
	}
	for _, foreign := range []string{"rival-only template"} {
		if strings.Contains(all.String(), foreign) {
			t.Errorf("export contains %q of another workspace", foreign)
		}
	}
	if !strings.Contains(all.String(), "exported text, edited") {
		t.Error("export lacks the template's current text")
	}
}
