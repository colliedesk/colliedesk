package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func kbTagCounts(t *testing.T, f kbFilterFixture) string {
	t.Helper()
	var page struct {
		Items []struct {
			Tag          string `json:"tag"`
			ArticleCount int    `json:"article_count"`
		} `json:"items"`
	}
	kbCall(t, f.api, http.MethodGet, "/api/v1/kb/tags", nil, f.admin, http.StatusOK, &page)
	return fmt.Sprint(page.Items)
}

// The tag filter offers the tags the workspace's articles have, by name with their counts: never a tag
// only an article in the trash has, never another workspace's.
func TestKnowledgeBaseTagsAreListedWithTheirArticleCounts(t *testing.T) {
	f := newKBFilterFixture(t)
	if got := kbTagCounts(t, f); got != "[{billing 2} {refund 1} {shipping 1}]" {
		t.Fatalf("tags = %s, want [{billing 2} {refund 1} {shipping 1}]", got)
	}
	parcel := kbGetArticle(t, f.api, f.admin, f.ids["parcel"])
	kbCall(t, f.api, http.MethodDelete, "/api/v1/kb/articles/"+parcel.ID+"?updated_at="+url.QueryEscape(parcel.UpdatedAt), nil, f.admin, http.StatusNoContent, nil)
	if got := kbTagCounts(t, f); got != "[{billing 2} {refund 1}]" {
		t.Fatalf("tags after trashing the parcel = %s, want [{billing 2} {refund 1}]", got)
	}
}
