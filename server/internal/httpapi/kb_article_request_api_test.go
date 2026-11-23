package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type kbRequestedGap struct {
	kbGapView
	ConversationCount int `json:"conversation_count"`
	Conversations     []struct {
		ConversationID string `json:"conversation_id"`
		DisplayRef     string `json:"display_ref"`
		Subject        string `json:"subject"`
	} `json:"conversations"`
	Signals []struct {
		Kind string  `json:"kind"`
		Text string  `json:"text"`
		Note *string `json:"note"`
	} `json:"signals"`
}

type kbArticleRequestView struct {
	Gap             kbRequestedGap `json:"gap"`
	Created         bool           `json:"created"`
	AddedToExisting bool           `json:"added_to_existing"`
}

var (
	anEmail      = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	sixOrMoreRun = regexp.MustCompile(`\d{6,}`)
)

func requestPath(conversation string) string {
	return "/api/v1/conversations/" + conversation + "/article-request"
}

func askForArticle(t *testing.T, d draftDesk, token, conversation string, body map[string]any, want int) kbArticleRequestView {
	t.Helper()
	var out kbArticleRequestView
	var sent any
	if body != nil {
		sent = body
	}
	kbCall(t, d.api, http.MethodPost, requestPath(conversation), sent, token, want, &out)
	return out
}

func (d draftDesk) conversation(t *testing.T, subject string) string {
	t.Helper()
	return create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": d.contactA, "subject": subject})
}

func TestArticleRequestDeduplicates(t *testing.T) {
	d := newDraftDesk(t)
	first, second, third := d.conversationA, d.conversation(t, "Locked out"), d.conversation(t, "Password help")

	body := map[string]any{"title": "Reset a password", "note": "Asked twice, called from 0912345678"}
	filed := askForArticle(t, d, d.adminA, first, body, http.StatusCreated)
	checked := d.conversation(t, "Contract check")
	validateContractResponse(t, newRequest(t, http.MethodPost, requestPath(checked), body, d.adminA, nil),
		serve(d.api, newRequest(t, http.MethodPost, requestPath(checked), body, d.adminA, nil)))
	kbCall(t, d.api, http.MethodDelete, requestPath(checked), nil, d.adminA, http.StatusNoContent, nil)
	gap := filed.Gap.ID
	if !filed.Created || filed.AddedToExisting || filed.Gap.Title != "Reset a password" || filed.Gap.Status != "open" ||
		strings.Join(filed.Gap.Kinds, ",") != "requested" {
		t.Fatalf("request = %+v, want a new open gap of kind requested titled Reset a password", filed)
	}

	// Before sending the second, the dialog looks for a similar open request and offers it.
	var similar kbGapsView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps?similar="+url.QueryEscape("How to reset my password"), nil, d.adminA, http.StatusOK, &similar)
	if len(similar.Items) == 0 || similar.Items[0].ID != gap {
		t.Fatalf("similar = %+v, want the Reset a password gap first", similar.Items)
	}
	added := askForArticle(t, d, d.adminA, second, map[string]any{"title": "How to reset my password", "gap_id": gap}, http.StatusCreated)
	if added.Gap.ID != gap || !added.AddedToExisting || !added.Created {
		t.Fatalf("added = %+v, want the conversation added to gap %s", added, gap)
	}
	// Without the offer a close wording still joins the open request.
	joined := askForArticle(t, d, d.adminA, third, map[string]any{"title": "how to reset my password?"}, http.StatusCreated)
	if joined.Gap.ID != gap || !joined.AddedToExisting {
		t.Fatalf("joined = %+v, want gap %s", joined, gap)
	}
	// Asking again with the same words changes nothing.
	again := askForArticle(t, d, d.adminA, first, body, http.StatusOK)
	if again.Created || again.Gap.ID != gap {
		t.Fatalf("again = %+v, want the same request, not created", again)
	}

	var detail kbRequestedGap
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps/"+gap, nil, d.adminA, http.StatusOK, &detail)
	ids := []string{}
	for _, c := range detail.Conversations {
		ids = append(ids, c.ConversationID)
		if c.DisplayRef == "" || c.Subject == "" {
			t.Fatalf("conversation %+v, want its code and subject", c)
		}
	}
	if detail.ConversationCount != 3 || len(ids) != 3 || !strings.Contains(strings.Join(ids, ","), first) ||
		!strings.Contains(strings.Join(ids, ","), second) || !strings.Contains(strings.Join(ids, ","), third) {
		t.Fatalf("detail = %d conversations %v, want the three that asked", detail.ConversationCount, ids)
	}
	for _, signal := range detail.Signals {
		if signal.Kind != "requested" {
			t.Fatalf("signal %+v, want only requests", signal)
		}
		if signal.Note != nil && sixOrMoreRun.MatchString(*signal.Note) {
			t.Fatalf("note %q keeps a long number", *signal.Note)
		}
	}

	// Undo takes one conversation back; the gap counts again.
	kbCall(t, d.api, http.MethodDelete, requestPath(third), nil, d.adminA, http.StatusNoContent, nil)
	kbCall(t, d.api, http.MethodDelete, requestPath(third), nil, d.adminA, http.StatusNotFound, nil)
	var listed struct {
		Items []kbRequestedGap `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps?kind=requested", nil, d.adminA, http.StatusOK, &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != gap || listed.Items[0].ConversationCount != 2 {
		t.Fatalf("requested gaps = %+v, want gap %s from 2 conversations", listed.Items, gap)
	}
}

func TestArticleRequestIsMaskedAndADismissedRequestIsNotJoined(t *testing.T) {
	d := newDraftDesk(t)
	if _, err := d.pool.Exec(context.Background(), `UPDATE contacts SET organization='Lovelace Engines' WHERE id=$1`, d.contactA); err != nil {
		t.Fatal(err)
	}
	// Left out, the title is the conversation's subject.
	plain := askForArticle(t, d, d.adminA, d.conversationA, nil, http.StatusCreated)
	if plain.Gap.Title != "Printer jam" {
		t.Fatalf("title = %q, want the subject Printer jam", plain.Gap.Title)
	}
	masked := askForArticle(t, d, d.adminA, d.conversation(t, "Billing"), map[string]any{
		"title": "Ada Lovelace at Lovelace Engines cannot read invoice 12345678 sent to ada@customer-a.example",
		"note":  "Ada says it is urgent"}, http.StatusCreated)
	var detail kbRequestedGap
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps/"+masked.Gap.ID, nil, d.adminA, http.StatusOK, &detail)
	text := masked.Gap.Title + " " + detail.Signals[0].Text + " " + *detail.Signals[0].Note
	for _, leak := range []string{"Ada", "Lovelace", "12345678", "ada@customer-a.example"} {
		if strings.Contains(text, leak) {
			t.Fatalf("request %q keeps %q", text, leak)
		}
	}
	if !strings.Contains(text, "invoice") {
		t.Fatalf("request %q lost its words", text)
	}

	// A dismissed request is not offered or joined: the next one opens a gap of its own.
	kbCall(t, d.api, http.MethodPatch, "/api/v1/kb/gaps/"+plain.Gap.ID, map[string]any{"status": "dismissed"}, d.adminA, http.StatusOK, nil)
	var similar kbGapsView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps?similar=Printer+jam", nil, d.adminA, http.StatusOK, &similar)
	if len(similar.Items) != 0 {
		t.Fatalf("similar = %v, want no dismissed gap offered", gapTitles(similar))
	}
	fresh := askForArticle(t, d, d.adminA, d.conversation(t, "Jammed"), map[string]any{"title": "Printer jam"}, http.StatusCreated)
	if fresh.Gap.ID == plain.Gap.ID || fresh.AddedToExisting || fresh.Gap.Status != "open" {
		t.Fatalf("fresh = %+v, want a new open gap, not the dismissed %s", fresh, plain.Gap.ID)
	}
	kbCall(t, d.api, http.MethodPost, requestPath(d.conversation(t, "Joined")), map[string]any{"gap_id": plain.Gap.ID}, d.adminA, http.StatusNotFound, nil)
	kbCall(t, d.api, http.MethodPost, requestPath(d.conversation(t, "Blank")), map[string]any{"title": " ?! "}, d.adminA, http.StatusBadRequest, nil)
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps?similar=jam&status=dismissed", nil, d.adminA, http.StatusBadRequest, nil)
}

func TestArticleRequestNeedsBothReadPermissionsAndStaysInItsWorkspace(t *testing.T) {
	d := newDraftDesk(t)
	kbOnly := customToken(t, d.api, d.adminA, "KB reader", "kb_articles:read")
	inboxOnly := customToken(t, d.api, d.adminA, "Inbox reader", "conversations:read")
	both := customToken(t, d.api, d.adminA, "Both readers", "conversations:read", "kb_articles:read")
	refused(t, d.api, http.MethodPost, requestPath(d.conversationA), map[string]any{}, kbOnly, "conversations:read")
	refused(t, d.api, http.MethodPost, requestPath(d.conversationA), map[string]any{}, inboxOnly, "kb_articles:read")
	filed := askForArticle(t, d, both, d.conversationA, map[string]any{"title": "Clear a jam"}, http.StatusCreated)
	refused(t, d.api, http.MethodDelete, requestPath(d.conversationA), nil, inboxOnly, "kb_articles:read")

	// Another workspace reads A's conversation, request and gap as missing.
	kbCall(t, d.api, http.MethodPost, requestPath(d.conversationA), map[string]any{"title": "Clear a jam"}, d.adminB, http.StatusNotFound, nil)
	kbCall(t, d.api, http.MethodDelete, requestPath(d.conversationA), nil, d.adminB, http.StatusNotFound, nil)
	kbCall(t, d.api, http.MethodPost, requestPath(d.convoB), map[string]any{"gap_id": filed.Gap.ID}, d.adminB, http.StatusNotFound, nil)
	kbCall(t, d.api, http.MethodPost, requestPath("not-a-conversation"), map[string]any{}, d.adminA, http.StatusNotFound, nil)
	var fromB kbGapsView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/gaps?similar=Clear+a+jam", nil, d.adminB, http.StatusOK, &fromB)
	if len(fromB.Items) != 0 {
		t.Fatalf("B finds %v, want none of A's gaps", gapTitles(fromB))
	}
	if n := queryString(t, d.pool, `SELECT count(*)::text FROM kb_gap_signals WHERE workspace_id=$1`, d.workspaceB); n != "0" {
		t.Fatalf("B has %s signals, want none", n)
	}
}

func TestADeletedConversationTakesItsRequestWithIt(t *testing.T) {
	d := newDraftDesk(t)
	gone := d.conversation(t, "Going away")
	filed := askForArticle(t, d, d.adminA, gone, map[string]any{"title": "Close my account"}, http.StatusCreated)
	if _, err := d.pool.Exec(context.Background(), `DELETE FROM conversations WHERE workspace_id=$1 AND id=$2`, d.workspaceA, gone); err != nil {
		t.Fatal(err)
	}
	if n := queryString(t, d.pool, `SELECT count(*)::text FROM kb_gaps WHERE id=$1`, filed.Gap.ID); n != "0" {
		t.Fatalf("gaps left = %s, want the gap gone with its only request", n)
	}
}
