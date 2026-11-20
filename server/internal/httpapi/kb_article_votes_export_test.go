package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

func TestTheExportKeepsEveryVoteAndResetOfAnArticle(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Export", "admin@vote-export.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Export help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Export a plan", "status": "published", "body_md": "Open Billing."})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	no := postVote(site, page+"/vote", url.Values{"helpful": {"no"}, "missing": {"Which file? Mail sam@example.org"}}, testOrigin, "192.0.2.90")
	reader := chatCookie(t, no)
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.91"), page+"?voted=yes#helpful")
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/votes/reset", nil, admin, http.StatusOK, nil)
	// The first reader changes their mind after the reset: a new current vote, the closed one stays.
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"yes"}}, testOrigin, "192.0.2.90", reader), page+"?voted=yes#helpful")

	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	resets := ndjsonLines(t, entries["kb-article-vote-resets.ndjson"])
	if len(resets) != 1 || resets[0]["article_id"] != article.ID || resets[0]["helpful"] != float64(1) || resets[0]["not_helpful"] != float64(1) ||
		resets[0]["reset_by_agent_id"] == nil || resets[0]["token_label"] != nil {
		t.Fatalf("resets = %+v, want one by the admin keeping 1 and 1", resets)
	}
	votes := ndjsonLines(t, entries["kb-article-votes.ndjson"])
	if len(votes) != 3 {
		t.Fatalf("votes = %+v, want the two closed by the reset and the one after it", votes)
	}
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	closed := 0
	for _, vote := range votes {
		_, contact := vote["contact_id"]
		_, address := vote["address"]
		if vote["article_id"] != article.ID || !hash.MatchString(fmt.Sprint(vote["voter"])) || contact || address {
			t.Fatalf("vote = %+v, want this article, a hex voter id, and neither a contact nor the address hash", vote)
		}
		if vote["reset_id"] == resets[0]["id"] {
			closed++
		}
	}
	first, last := votes[0], votes[2]
	if closed != 2 || first["helpful"] != false || first["missing"] != "Which file? Mail [email removed]" || last["reset_id"] != nil || last["helpful"] != true || last["voter"] != first["voter"] {
		t.Fatalf("votes = %+v, want the closed No with its masked line first and the same reader's current Yes last", votes)
	}
	for name, content := range entries {
		if bytes.Contains(content, []byte("sam@example.org")) {
			t.Fatalf("%s holds the email the reader typed", name)
		}
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_article_votes"] != 3 || manifest.Counts["kb_article_vote_resets"] != 1 {
		t.Fatalf("manifest counts = %+v, want 3 votes and 1 reset", manifest.Counts)
	}
}
