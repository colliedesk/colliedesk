package httpapi_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

// An article ends with "Still need help?": the ways to reach the team the workspace set up, each one
// naming the article, as plain links. A help center with none of them shows no block.
func TestAnArticleEndsWithTheWaysToReachTheTeamThatAreSetUp(t *testing.T) {
	d := newChatDesk(t, "Help Mike", nil)
	articlePath := d.base + "/a/" + num(d.article.Number) + "-" + d.article.Slug
	read := func() string {
		t.Helper()
		page := helpCenterCall(d.site, http.MethodGet, testOriginHost, articlePath)
		if page.Code != http.StatusOK {
			t.Fatalf("article status=%d", page.Code)
		}
		return html.UnescapeString(page.Body.String())
	}

	// The chat alone: Ask AI opens the chat page about this article.
	body := read()
	for _, want := range []string{`<section class="still-help" aria-labelledby="still-help-title">`, `<h2 id="still-help-title" class="still-help-title">Still need help?</h2>`,
		`<a class="still-help-ask" href="/help/help-mike/chat?article=1">Ask AI</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("article lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "still-help-contact") || strings.Contains(body, "still-help-phone") {
		t.Fatalf("article offers a contact the workspace did not set up:\n%s", body)
	}

	// An email contact gets a message about the article, the phone its number.
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"contact_url": "mailto:help@mike.example", "contact_phone": "+1 (555) 010-2030", "custom_css": nil}, d.admin, http.StatusOK, nil)
	body = read()
	for _, want := range []string{
		`<a class="still-help-contact" href="mailto:help@mike.example?subject=Question%20about%3A%20Shipping%20times&body=About%20this%20article%3A%20https%3A%2F%2Fdesk.example.com%2Fhelp%2Fhelp-mike%2Fa%2F1-shipping-times%0A%0A">Email help@mike.example</a>`,
		`<a class="still-help-phone" href="tel:+15550102030">Call +1 (555) 010-2030</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("article lacks %q:\n%s", want, body)
		}
	}

	// A contact page on the team's site is linked as it is.
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"contact_url": "https://mike.example/contact", "custom_css": nil}, d.admin, http.StatusOK, nil)
	if body = read(); !strings.Contains(body, `<a class="still-help-contact" href="https://mike.example/contact">Contact us</a>`) {
		t.Fatalf("article lacks the contact page:\n%s", body)
	}

	// Without the chat the block is plain links: the page keeps the strict policy byte for byte.
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/chat", map[string]any{"source": "server", "enabled": false}, d.admin, http.StatusOK, nil)
	page := helpCenterCall(d.site, http.MethodGet, testOriginHost, articlePath)
	if !strings.Contains(page.Body.String(), `class="still-help-contact"`) {
		t.Fatalf("article without the chat lacks the contact page:\n%s", page.Body.String())
	}
	if got := page.Header().Get("Content-Security-Policy"); got != strictHelpCenterPolicy {
		t.Fatalf("article CSP with Still need help = %q, want %q", got, strictHelpCenterPolicy)
	}

	// Nothing set up: no block.
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"custom_css": nil}, d.admin, http.StatusOK, nil)
	if body = read(); strings.Contains(body, "still-help") {
		t.Fatalf("article shows Still need help with nothing set up:\n%s", body)
	}
}

// The chat page opened from an article is about it, and a hand-off from the chat carries the article to
// the team. A number that is no published article is ignored.
func TestARequestFromTheChatCarriesTheArticle(t *testing.T) {
	d := newChatDesk(t, "Help November", nil)
	draft := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": d.article.CollectionID, "title": "Secret plans", "status": "draft", "body_md": "Not yet."})

	page := d.call(t, http.MethodGet, "/chat?article="+num(d.article.Number), nil)
	if body := html.UnescapeString(page.Body.String()); page.Code != http.StatusOK || !strings.Contains(body, `"article":{"number":1,"title":"Shipping times"}`) {
		t.Fatalf("chat page about the article: status=%d body=%.600s", page.Code, body)
	}
	for _, other := range []string{num(draft.Number), "99", "x"} {
		if body := html.UnescapeString(d.call(t, http.MethodGet, "/chat?article="+other, nil).Body.String()); strings.Contains(body, `"article":`) || strings.Contains(body, "Secret plans") {
			t.Fatalf("chat page about %s names an article:\n%.600s", other, body)
		}
	}

	handoff := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "reader@example.com", "article_number": d.article.Number})
	if handoff.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", handoff.Code, handoff.Body.String())
	}
	secret := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "other@example.com", "article_number": draft.Number})
	if secret.Code != http.StatusCreated {
		t.Fatalf("handoff of a draft status=%d body=%s", secret.Code, secret.Body.String())
	}
	var conversations struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations", nil, d.admin, http.StatusOK, &conversations)
	if len(conversations.Items) != 2 {
		t.Fatalf("%d conversations, want 2", len(conversations.Items))
	}
	var bodies []string
	for _, conversation := range conversations.Items {
		var timeline struct {
			Items []struct {
				Body string `json:"body"`
			} `json:"items"`
		}
		kbCall(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation.ID+"/messages", nil, d.admin, http.StatusOK, &timeline)
		bodies = append(bodies, timeline.Items[0].Body)
	}
	joined := strings.Join(bodies, "\n----\n")
	if strings.Count(joined, "Article: Shipping times https://desk.example.com/help/help-november/a/1") != 1 || strings.Contains(joined, "Secret plans") ||
		strings.Count(joined, "Article:") != 1 {
		t.Fatalf("hand-off messages:\n%s", joined)
	}
}
