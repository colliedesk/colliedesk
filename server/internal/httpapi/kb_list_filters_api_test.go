package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// Every list filter, alone and with others, returns exactly the fixture articles it describes. The
// expected names are written out by hand from the fixture, never computed.
func TestTheKnowledgeBaseListFiltersReturnTheMatchingArticles(t *testing.T) {
	f := newKBFilterFixture(t)
	writer, admin := f.writerID, f.adminID
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"card", "invoice", "parcel", "label", "lost"}},
		{"q=parcel", []string{"parcel"}},
		// Status: draft, in review, approved, published, with unpublished changes.
		{"status=draft", []string{"invoice", "label", "lost"}},
		{"state=in_review", []string{"label"}},
		{"state=approved", []string{"lost"}},
		{"status=published", []string{"card", "parcel"}},
		{"has_draft=true", []string{"parcel"}},
		// Collection: its own articles, or with its collections at every depth and the articles listed there.
		{"collection_id=" + f.billing, []string{"invoice"}},
		{"collection_id=" + f.billing + "&include_subcollections=true", []string{"card", "invoice", "parcel"}},
		{"collection_id=" + f.refunds + "&include_subcollections=true", []string{"card", "parcel"}},
		{"collection_id=" + f.shipping + "&include_subcollections=true", []string{"parcel", "label", "lost"}},
		{"author=" + writer, []string{"card", "parcel"}},
		{"author=" + writer + "," + admin, []string{"card", "invoice", "parcel", "label", "lost"}},
		// Tags are cleaned as they are saved; any of them, or all of them.
		{"tag=billing", []string{"card", "invoice"}},
		{"tag=BILLING", []string{"card", "invoice"}},
		{"tag=refund&tag=shipping", []string{"card", "parcel"}},
		{"tag=billing&tag=refund&tag_match=all", []string{"card"}},
		{"tag=billing&tag=shipping&tag_match=all", []string{}},
		{"tag=returns", []string{}},
		// Dates are half-open; a date is midnight in the workspace's timezone (UTC+7).
		{"created_after=2026-11-21", []string{"card"}},
		{"created_after=2026-11-21T00:00:00%2B07:00", []string{"card"}},
		{"created_before=2026-11-21", []string{"invoice", "parcel", "label", "lost"}},
		{"created_after=2026-11-01&created_before=2026-11-21", []string{"invoice", "label"}},
		{"created_after=2026-11-20T17:30:00Z", []string{"card"}},
		{"created_before=2026-11-20T17:30:00Z", []string{"invoice", "parcel", "label", "lost"}},
		{"updated_after=2026-11-14", []string{"parcel"}},
		{"updated_before=2026-10-15", []string{"label", "lost"}},
		// Language: without a locale, any language of the help center.
		{"translation=missing", []string{"parcel", "label", "lost"}},
		{"translation=missing&locale=fr", []string{"parcel", "label", "lost"}},
		{"translation=outdated", []string{"card"}},
		{"translation=outdated&locale=fr", []string{"card"}},
		// Together.
		{"author=" + writer + "&tag=billing", []string{"card"}},
		{"collection_id=" + f.billing + "&include_subcollections=true&status=draft", []string{"invoice"}},
		{"tag=shipping&created_before=2026-11-01&has_draft=true", []string{"parcel"}},
		{"translation=missing&author=" + admin, []string{"label", "lost"}},
		{"updated_after=2026-11-11&tag=billing", []string{"invoice"}},
		{"q=parcel&collection_id=" + f.shipping + "&include_subcollections=true", []string{"parcel"}},
	}
	for _, c := range cases {
		if got := f.names(t, c.query); !slices.Equal(got, c.want) {
			t.Errorf("%q = %v, want %v", c.query, got, c.want)
		}
	}
}

// Another workspace's author or collection, or an unknown one, gives an empty list: never an error
// that would tell the caller the id exists somewhere.
func TestKnowledgeBaseFiltersOnAnotherWorkspacesIDsReturnNothing(t *testing.T) {
	f := newKBFilterFixture(t)
	for _, query := range []string{
		"author=" + f.foreignAgentID,
		"author=00000000-0000-4000-8000-000000000000",
		"collection_id=" + f.foreignCollectionID,
		"collection_id=" + f.foreignCollectionID + "&include_subcollections=true",
		"collection_id=00000000-0000-4000-8000-000000000000&include_subcollections=true",
	} {
		if got := f.names(t, query); len(got) != 0 {
			t.Errorf("%q = %v, want no articles", query, got)
		}
	}
}

// A filter value the server cannot read is refused with a message that says how to write it.
func TestKnowledgeBaseFiltersRefuseValuesTheyCannotRead(t *testing.T) {
	f := newKBFilterFixture(t)
	for _, query := range []string{
		"author=writer", "tag_match=some", "created_after=yesterday", "updated_before=2026-13-01",
		"translation=gone", "translation=missing&locale=de", "locale=fr", "include_subcollections=true",
	} {
		response := request(t, f.api, http.MethodGet, "/api/v1/kb/articles?"+query, nil, f.admin, nil)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%q status=%d body=%s, want 400", query, response.Code, response.Body.String())
		}
	}
}

// The list says which translations each article has and which are outdated.
func TestKnowledgeBaseListShowsEachArticlesLanguages(t *testing.T) {
	f := newKBFilterFixture(t)
	type language struct {
		Locale   string `json:"locale"`
		Outdated bool   `json:"outdated"`
	}
	var page struct {
		Items []struct {
			ID        string     `json:"id"`
			Languages []language `json:"languages"`
		} `json:"items"`
	}
	kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles", nil, f.admin, http.StatusOK, &page)
	want := map[string][]language{"card": {{"fr", true}}, "invoice": {{"fr", false}}, "parcel": {}, "label": {}, "lost": {}}
	for _, item := range page.Items {
		for name, id := range f.ids {
			if id == item.ID && !slices.Equal(item.Languages, want[name]) {
				t.Errorf("%s languages = %+v, want %+v", name, item.Languages, want[name])
			}
		}
	}
}

// The list says which articles wait for a reviewer and which a reviewer approved, so the status
// column reads draft, in review, approved, published or with unpublished changes without another call.
func TestKnowledgeBaseListShowsEachArticlesReviewState(t *testing.T) {
	f := newKBFilterFixture(t)
	var page struct {
		Items []struct {
			ID          string  `json:"id"`
			ReviewState *string `json:"review_state"`
		} `json:"items"`
	}
	kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles", nil, f.admin, http.StatusOK, &page)
	want := map[string]string{"card": "", "invoice": "", "parcel": "", "label": "in_review", "lost": "approved"}
	seen := 0
	for _, item := range page.Items {
		for name, id := range f.ids {
			if id != item.ID {
				continue
			}
			seen++
			got := ""
			if item.ReviewState != nil {
				got = *item.ReviewState
			}
			if got != want[name] {
				t.Errorf("%s review_state = %q, want %q", name, got, want[name])
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("saw %d fixture articles, want %d", seen, len(want))
	}
}
