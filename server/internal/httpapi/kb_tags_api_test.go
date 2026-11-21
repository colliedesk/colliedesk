package httpapi_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// kbTaggedArticle is an article with its live tags and, when it has unpublished changes, the tags its
// draft would publish.
type kbTaggedArticle struct {
	kbArticleView
	Tags     []string `json:"tags"`
	HasDraft bool     `json:"has_draft"`
	Draft    *struct {
		Tags      []string `json:"tags"`
		UpdatedAt string   `json:"updated_at"`
	} `json:"draft"`
}

// An article's tags are trimmed, lowercased, deduplicated and sorted; a tag longer than 40
// characters, an empty tag and a 21st tag are refused and nothing changes.
func TestKBArticleTagsAreNormalisedAndCapped(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Tags", "admin@kb-tags.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})

	var created kbTaggedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Refunds",
		"body_md": "Within 14 days.", "tags": []string{" Billing ", "billing", "REFUNDS"}}, admin, http.StatusCreated, &created)
	if !slices.Equal(created.Tags, []string{"billing", "refunds"}) {
		t.Fatalf("created tags = %q, want [billing refunds]", created.Tags)
	}

	var edited kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": created.UpdatedAt,
		"tags": []string{"Vat", "  card payments", "vat"}}, admin, http.StatusOK, &edited)
	if !slices.Equal(edited.Tags, []string{"card payments", "vat"}) || edited.HasDraft {
		t.Fatalf("edited tags = %q has_draft=%v, want [card payments vat] and no draft", edited.Tags, edited.HasDraft)
	}
	var read kbTaggedArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID, nil, admin, http.StatusOK, &read)
	if !slices.Equal(read.Tags, []string{"card payments", "vat"}) {
		t.Fatalf("GET tags = %q", read.Tags)
	}

	tooMany := []string{}
	for i := range 21 {
		tooMany = append(tooMany, fmt.Sprintf("tag-%02d", i))
	}
	for name, tags := range map[string][]string{
		"41 characters": {strings.Repeat("a", 41)},
		"empty":         {"refunds", "   "},
		"21 tags":       tooMany,
	} {
		response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": read.UpdatedAt, "tags": tags}, admin, nil)
		if response.Code != http.StatusBadRequest || errorCode(t, response) != "invalid_request" {
			t.Errorf("%s: status=%d body=%s, want 400 invalid_request", name, response.Code, response.Body.String())
		}
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": read.UpdatedAt,
		"tags": tooMany[:20]}, admin, http.StatusOK, &edited)
	if len(edited.Tags) != 20 {
		t.Fatalf("20 tags saved %d", len(edited.Tags))
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": edited.UpdatedAt,
		"tags": []string{strings.Repeat("b", 40)}}, admin, http.StatusOK, &edited)
	if !slices.Equal(edited.Tags, []string{strings.Repeat("b", 40)}) {
		t.Fatalf("a 40-character tag saved %q", edited.Tags)
	}
}

// A tag change to a published article is part of its draft: the article keeps its live tags, and the
// draft's tags replace them only on publish. Removing every tag and publishing leaves none.
func TestATagChangeToAPublishedArticleWaitsInTheDraft(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Tag Drafts", "admin@kb-tag-drafts.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	var live kbTaggedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Refunds",
		"body_md": "Within 14 days.", "status": "published", "tags": []string{"refunds"}}, admin, http.StatusCreated, &live)

	var saved kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"tags": []string{"refunds", "money back"}}, admin, http.StatusOK, &saved)
	if !slices.Equal(saved.Tags, []string{"refunds"}) || !saved.HasDraft || saved.Draft == nil ||
		!slices.Equal(saved.Draft.Tags, []string{"money back", "refunds"}) {
		t.Fatalf("after a tag save: live=%q draft=%+v", saved.Tags, saved.Draft)
	}
	// A draft that changes the body only keeps the draft's tags.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.UpdatedAt,
		"body_md": "Within 30 days."}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || !slices.Equal(saved.Draft.Tags, []string{"money back", "refunds"}) {
		t.Fatalf("a body save lost the draft's tags: %+v", saved.Draft)
	}

	var published kbTaggedArticle
	kbPublishDraft(t, api, admin, live.ID, saved.UpdatedAt, &published)
	if !slices.Equal(published.Tags, []string{"money back", "refunds"}) || published.HasDraft {
		t.Fatalf("published tags = %q has_draft=%v", published.Tags, published.HasDraft)
	}

	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": published.UpdatedAt,
		"tags": []string{}}, admin, http.StatusOK, &saved)
	if !slices.Equal(saved.Tags, []string{"money back", "refunds"}) || saved.Draft == nil || len(saved.Draft.Tags) != 0 {
		t.Fatalf("after removing every tag in the draft: live=%q draft=%+v", saved.Tags, saved.Draft)
	}
	kbPublishDraft(t, api, admin, live.ID, saved.UpdatedAt, &published)
	if len(published.Tags) != 0 {
		t.Fatalf("publishing a draft with no tags left %q", published.Tags)
	}
}
