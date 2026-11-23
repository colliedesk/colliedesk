package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// A "No" with an address opens its conversation on the help center the reader voted on, so the inbox
// and Results count it there.
func TestArticleFeedbackOpensOnTheHelpCenterVotedOn(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Feedback Shelves", "admin@feedback-shelves.example.com")
	slug, workspace := workspaceSlug(t, api, admin), workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Main answers", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Main answers", "locale": "en", "feedback_conversations": true},
		admin, http.StatusOK, nil)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	primary := queryString(t, pool, `SELECT primary_help_center_id($1)::text`, workspace)

	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	jam := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Clear a paper jam", "status": "published", "body_md": "Open tray two."})
	var cables placedCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Cables", "help_center_ids": []string{hardware.ID}},
		admin, http.StatusCreated, &cables)
	swap := kbArticle(t, api, admin, map[string]any{"collection_id": cables.ID, "title": "Swap a cable", "status": "published", "body_md": "Unplug it."})

	site := helpCenterSite(api)
	for _, vote := range []struct{ root, address string }{
		{"/help/" + slug, "198.51.100.61"},
		{"/help/" + slug + "/hardware", "198.51.100.62"},
	} {
		article := jam
		if vote.root != "/help/"+slug {
			article = swap
		}
		page := fmt.Sprintf("%s/a/%d-%s", vote.root, article.Number, article.Slug)
		form := url.Values{"helpful": {"no"}, "missing": {"A step is missing"}, "email": {"reader@feedback.example"}}
		wantSeeOther(t, postVote(site, page+"/vote", form, testOrigin, vote.address), page+"?voted=sent#helpful")
	}

	for subject, want := range map[string]string{"Article feedback: Clear a paper jam": primary, "Article feedback: Swap a cable": hardware.ID} {
		got := queryString(t, pool, `SELECT COALESCE(help_center_id::text, '') FROM conversations WHERE workspace_id=$1 AND subject=$2`, workspace, subject)
		if got != want {
			t.Errorf("%s: help center %q, want %q", subject, got, want)
		}
	}
}
