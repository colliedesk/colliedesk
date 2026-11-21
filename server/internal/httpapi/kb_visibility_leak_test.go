package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
)

// kbKeptArticle is a published article that the help center must never show, with its image and file.
type kbKeptArticle struct {
	article       kbArticleView
	page, file    string
	image, fileID string
}

// publishKeptArticle publishes an article with visibility, its unique word, an image and a file.
func publishKeptArticle(t *testing.T, api http.Handler, admin, base, collectionID, visibility, title, word string, n byte) kbKeptArticle {
	t.Helper()
	image := uploadKBImage(t, api, admin, otherPNG(60+n), http.StatusCreated)
	file := uploadKBFile(t, api, admin, word+" sheet.pdf", []byte("%PDF-1.4 "+word))
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collectionID, "title": title, "visibility": visibility,
		"body_md": "Only " + word + " here.\n\n![" + word + "](" + image.URL + ")"})
	listed := putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{file.ID}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"status": "published", "updated_at": listed.UpdatedAt}, admin, http.StatusOK, &article)
	return kbKeptArticle{article: article, page: fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug),
		file: base + "/files/" + file.ID + "/" + word + "-sheet.pdf", image: base + "/images/" + image.ID, fileID: file.ID}
}

// A published article kept for the team or for help chat, directly or through its collection, never
// reaches a help center reader: no page, list, search, related list, sitemap.xml, llms.txt, .md copy,
// structured data, image, file, vote or old Freshdesk address shows it or tells it from a missing one.
func TestArticlesKeptFromTheHelpCenterNeverReachIt(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Kept Articles", "admin@kept-articles.example.com")
	slug := workspaceSlug(t, api, admin)
	base := "/help/" + slug
	enableHelpCenter(t, api, admin, "Kept help", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)

	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	plain := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Change your password", "status": "published", "body_md": "Open settings."})
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Change your email", "status": "published", "body_md": "Open your profile."})
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+plain.ID+"/translations", map[string]any{"locale": "fr", "title": "Changer le mot de passe",
		"status": "published", "body_md": "Ouvrez les réglages."}, admin, http.StatusCreated, nil)
	team := publishKeptArticle(t, api, admin, base, guides.ID, "agents", "Refund overrides", "quokkaword", 1)
	chat := publishKeptArticle(t, api, admin, base, guides.ID, "help_chat", "Delivery windows", "narwhalword", 2)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+team.article.ID+"/translations", map[string]any{"locale": "fr", "title": "Remboursements lemurword",
		"status": "published", "body_md": "lemurword"}, admin, http.StatusCreated, nil)
	shelf := kbCollection(t, api, admin, map[string]any{"name": "Wombatshelf", "description": "wombatword", "visibility": "agents"})
	shelved := kbArticle(t, api, admin, map[string]any{"collection_id": shelf.ID, "title": "Shelf note", "status": "published", "body_md": "wombatword"})

	secrets := []string{"quokkaword", "narwhalword", "lemurword", "wombatword", "Wombatshelf", "Refund overrides", "Delivery windows", "Shelf note",
		team.fileID, chat.fileID, team.image, chat.image}
	plainPage := fmt.Sprintf("%s/a/%d-%s", base, plain.Number, plain.Slug)
	for _, path := range []string{base, base + "/c/guides", plainPage, plainPage + ".md", base + "/sitemap.xml", base + "/llms.txt", base + "/fr", base + "/fr/c/guides"} {
		wantPage(t, visit(site, path, "192.0.2.140"), http.StatusOK, nil, secrets)
	}
	// A search page repeats its own words in the search box, so each one is held to the others.
	for _, word := range []string{"quokkaword", "narwhalword", "lemurword", "wombatword", "shelf"} {
		others := slices.DeleteFunc(slices.Clone(secrets), func(secret string) bool { return secret == word })
		wantPage(t, visit(site, base+"/search?q="+word, "192.0.2.140"), http.StatusOK, nil, others)
	}
	// The plain page still lists what the reader may see: it is the related list and the structured data under test.
	wantPage(t, visit(site, plainPage, "192.0.2.140"), http.StatusOK, []string{"Change your email", "application/ld+json"}, nil)

	missing := visit(site, fmt.Sprintf("%s/a/%d-gone", base, shelved.Number+100), "192.0.2.140")
	missingFile := visit(site, base+"/files/00000000-0000-4000-8000-000000000001/nothing.pdf", "192.0.2.140")
	wantMissing := func(path string, like string) {
		t.Helper()
		if response := visit(site, path, "192.0.2.140"); response.Code != http.StatusNotFound || response.Body.String() != like {
			t.Errorf("GET %s = %d, want the 404 of a missing item; body=%s", path, response.Code, response.Body.String())
		}
	}
	for _, kept := range []kbKeptArticle{team, chat} {
		wantMissing(kept.page, missing.Body.String())
		wantMissing(kept.page+".md", missing.Body.String())
		wantMissing(kept.file, missingFile.Body.String())
		if response := visit(site, kept.image, "192.0.2.140"); response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", kept.image, response.Code)
		}
		if response := postVote(site, kept.page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.141"); response.Code != http.StatusNotFound {
			t.Errorf("vote on %s = %d, want 404", kept.page, response.Code)
		}
	}
	wantMissing(fmt.Sprintf("%s/a/%d-%s", base, shelved.Number, shelved.Slug), missing.Body.String())
	if response := visit(site, base+"/c/wombatshelf", "192.0.2.140"); response.Code != http.StatusNotFound {
		t.Errorf("an agents-only collection = %d, want 404", response.Code)
	}
	frMissing := visit(site, fmt.Sprintf("%s/fr/a/%d-gone", base, shelved.Number+100), "192.0.2.140")
	if response := visit(site, fmt.Sprintf("%s/fr/a/%d-remboursements-lemurword", base, team.article.Number), "192.0.2.140"); response.Code != frMissing.Code ||
		response.Header().Get("Location") != "" && response.Header().Get("Location") != frMissing.Header().Get("Location") {
		t.Errorf("French copy of an agents-only article = %d %q, want %d like a missing one", response.Code, response.Header().Get("Location"), frMissing.Code)
	}
	if response := postVote(site, plainPage+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.142"); response.Code != http.StatusSeeOther {
		t.Errorf("vote on a public article = %d, want 303", response.Code)
	}

	// An old Freshdesk address of a kept article answers as an id nobody imported.
	ledgerRow(t, pool, slug, "freshdesk:account:1", "kb_article", "7301", team.article.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, pool, slug, "freshdesk:account:1", "kb_folder", "2301", shelf.ID, "done", "2026-10-20 10:00:00+00")
	home := testOrigin + base
	wantMove(t, site, base+"/support/solutions/articles/7301-refund-overrides", http.StatusFound, home+"/search?q=refund+overrides")
	wantMove(t, site, base+"/support/solutions/folders/2301-wombatshelf", http.StatusFound, home+"/search?q=wombatshelf")
}

// An everyone article in an agents-only collection is not public; moved out, it is.
func TestAnArticleIsAsRestrictedAsItsCollection(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Kept Inherit", "admin@kept-inherit.example.com")
	base := "/help/" + workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Inherit help", true)
	open := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	team := kbCollection(t, api, admin, map[string]any{"name": "Team notes", "visibility": "agents"})
	inner := kbCollection(t, api, admin, map[string]any{"name": "Inner", "parent_id": team.ID})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": inner.ID, "title": "Escalation ladder", "status": "published", "body_md": "ibexword"})
	page := fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug)
	wantPage(t, visit(site, page, "192.0.2.150"), http.StatusNotFound, nil, []string{"ibexword"})
	wantPage(t, visit(site, base+"/search?q=ibexword", "192.0.2.150"), http.StatusOK, nil, []string{"Escalation ladder"})

	// A published article moves through its draft, so the move shows once the draft is published.
	var moved kbArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "collection_id": open.ID}, admin, http.StatusOK, &moved)
	wantPage(t, visit(site, page, "192.0.2.150"), http.StatusNotFound, nil, []string{"ibexword"})
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": moved.UpdatedAt}, admin, http.StatusOK, nil)
	wantPage(t, visit(site, page, "192.0.2.150"), http.StatusOK, []string{"ibexword"}, nil)
	wantPage(t, visit(site, base+"/search?q=ibexword", "192.0.2.150"), http.StatusOK, []string{"Escalation ladder"}, nil)
}
