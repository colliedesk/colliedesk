package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const spamPath = "/api/v1/conversations/spam"

type spamResult struct {
	Updated int `json:"updated"`
	Senders []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"senders"`
}

func markSpam(t *testing.T, api http.Handler, token string, spam bool, ids ...string) spamResult {
	t.Helper()
	response := request(t, api, http.MethodPut, spamPath, map[string]any{"ids": ids, "spam": spam}, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("POST spam=%v %v: %d %s", spam, ids, response.Code, response.Body.String())
	}
	var result spamResult
	decode(t, response, &result)
	return result
}

func listedStatuses(t *testing.T, api http.Handler, token, query string) map[string]string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list %q: %d %s", query, response.Code, response.Body.String())
	}
	var page struct {
		Items []conversationSummary `json:"items"`
	}
	decode(t, response, &page)
	statuses := map[string]string{}
	for _, item := range page.Items {
		statuses[item.ID] = item.Status
	}
	return statuses
}

func spamMail(from, id, subject string) string {
	return mail([]string{"From: " + from, "To: support@spam.example.com", "Subject: " + subject, "Message-ID: <" + id + "@sender.example>"}, subject+" body.")
}

// Spam leaves the inbox and every view but Spam, stops its clocks and leaves Results; the sender's next mail
// is filed in Spam without telling anyone. Not spam puts the conversation back where it was and lets the
// sender's next mail reach the inbox.
func TestSpamLeavesTheInboxClocksAndResultsAndFilesTheSendersNextMail(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Spam Desk", "admin@spam.example.com")
	secret := connectEmail(t, api, admin, "support@spam.example.com")
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", roundTheClock(), admin, nil); response.Code != http.StatusOK {
		t.Fatalf("targets: %d %s", response.Code, response.Body.String())
	}
	spam := *inboundOK(t, api, secret, spamMail("Cheap Pills <pills@sender.example>", "pills-1", "Cheap pills")).ConversationID
	real := *inboundOK(t, api, secret, spamMail("Dana <dana@customer.example>", "dana-1", "Login loop")).ConversationID

	var cursor struct {
		Now time.Time `json:"now"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/inbox/activity", nil, admin, nil), &cursor)

	result := markSpam(t, api, admin, true, spam)
	if result.Updated != 1 || len(result.Senders) != 1 || result.Senders[0].Email != "pills@sender.example" || result.Senders[0].Name != "Cheap Pills" {
		t.Fatalf("mark spam = %+v", result)
	}
	if again := markSpam(t, api, admin, true, spam); again.Updated != 0 {
		t.Fatalf("marking it again changed %d", again.Updated)
	}
	for _, query := range []string{"", "status=open", "status=open,pending,resolved,closed", "search=pills", "contact=" + queryString(t, pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, spam)} {
		if listed := listedStatuses(t, api, admin, query); listed[spam] != "" {
			t.Fatalf("list %q still shows the spam: %v", query, listed)
		}
	}
	if listed := listedStatuses(t, api, admin, "status=spam"); len(listed) != 1 || listed[spam] != "spam" {
		t.Fatalf("Spam view = %v", listed)
	}
	var detail slaConversation
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+spam, nil, admin, nil), &detail)
	if detail.SLA == nil || detail.SLA.FirstResponse == nil || detail.SLA.FirstResponse.State != "stopped" || detail.SLA.FirstResponse.DueAt != nil {
		t.Fatalf("spam clocks = %+v", detail.SLA)
	}
	if sla := listedStatuses(t, api, admin, "status=spam&sla=overdue,due_soon"); len(sla) != 0 {
		t.Fatalf("spam counted in the clock filters: %v", sla)
	}

	// The sender's next mail opens a conversation straight in Spam, with no clocks and no notification.
	next := inboundOK(t, api, secret, spamMail("pills@sender.example", "pills-2", "More pills"))
	if next.Status != "created" {
		t.Fatalf("next mail = %+v", next)
	}
	if listed := listedStatuses(t, api, admin, "status=spam"); listed[*next.ConversationID] != "spam" {
		t.Fatalf("the sender's next mail is not in Spam: %v", listed)
	}
	if listed := listedStatuses(t, api, admin, ""); listed[*next.ConversationID] != "" {
		t.Fatalf("the sender's next mail reached the inbox: %v", listed)
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+*next.ConversationID, nil, admin, nil), &detail)
	if detail.SLA != nil && detail.SLA.FirstResponse != nil && detail.SLA.FirstResponse.State != "stopped" {
		t.Fatalf("the sender's next mail runs a clock: %+v", detail.SLA.FirstResponse)
	}
	// It is not a new conversation nor a customer reply to anyone; it only refreshes the lists (updated).
	var activity struct {
		New     []struct{ ID string } `json:"new_conversations"`
		Updated []struct {
			ID         string     `json:"id"`
			IncomingAt *time.Time `json:"incoming_at"`
		} `json:"updated"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/inbox/activity?since="+url.QueryEscape(cursor.Now.Format(time.RFC3339Nano)), nil, admin, nil), &activity)
	for _, item := range activity.New {
		if item.ID == *next.ConversationID {
			t.Fatalf("the sender's next mail notifies as new: %+v", activity)
		}
	}
	for _, item := range activity.Updated {
		if item.ID == *next.ConversationID && item.IncomingAt != nil {
			t.Fatalf("the sender's next mail notifies as a reply: %+v", activity)
		}
	}

	// Results count the real customer only.
	runResultsWorker(t, pool)
	var page resultsPage
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.Current == nil || page.Current.Windows["30d"].FirstResponse.Waiting != 1 {
		t.Fatalf("results count spam: %+v", page.Current)
	}
	code, listed := listedIDs(t, api, admin, "result="+page.Current.ID+":30d.first_response_waiting")
	if code != http.StatusOK || len(listed) != 1 || listed[0] != real {
		t.Fatalf("results waiting list = %d %v, want only %s", code, listed, real)
	}

	// A spam conversation's status is changed by Not spam only; spam is never set as a status.
	for _, attempt := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPatch, "/api/v1/conversations/" + spam, map[string]any{"status": "open"}},
		{http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": []string{spam, real}, "changes": map[string]any{"status": "resolved"}}},
	} {
		response := request(t, api, attempt.method, attempt.path, attempt.body, admin, nil)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"conversation_spam"`) || !strings.Contains(response.Body.String(), spamPath) {
			t.Fatalf("%s %s on spam = %d %s", attempt.method, attempt.path, response.Code, response.Body.String())
		}
	}
	if listed := listedStatuses(t, api, admin, ""); listed[real] != "open" {
		t.Fatalf("a refused bulk change changed the other conversation: %v", listed)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+real, map[string]any{"status": "spam"}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status spam = %d %s", response.Code, response.Body.String())
	}

	// Not spam: back where it was, and the sender's next mail reaches the inbox.
	if back := markSpam(t, api, admin, false, spam); back.Updated != 1 || len(back.Senders) != 1 || back.Senders[0].Email != "pills@sender.example" {
		t.Fatalf("not spam = %+v", back)
	}
	if listed := listedStatuses(t, api, admin, ""); listed[spam] != "open" || listed[*next.ConversationID] != "" {
		t.Fatalf("after Not spam = %v", listed)
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+spam, nil, admin, nil), &detail)
	if detail.SLA == nil || detail.SLA.FirstResponse == nil || detail.SLA.FirstResponse.State != "running" {
		t.Fatalf("clocks after Not spam = %+v", detail.SLA)
	}
	welcome := inboundOK(t, api, secret, spamMail("pills@sender.example", "pills-3", "A real question"))
	if listed := listedStatuses(t, api, admin, "status=open"); listed[*welcome.ConversationID] != "open" {
		t.Fatalf("the sender's mail after Not spam = %v", listed)
	}
}

// Not spam puts a conversation back in the status it had: a resolved one comes back resolved.
func TestNotSpamRestoresTheStatusItHad(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Restore Desk", "admin@restore.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Rae", "email": "rae@customer.example"})
	id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Old"})
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+id, map[string]any{"status": "resolved"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("resolve: %d", response.Code)
	}
	markSpam(t, api, admin, true, id)
	markSpam(t, api, admin, false, id)
	if listed := listedStatuses(t, api, admin, "status=resolved"); listed[id] != "resolved" {
		t.Fatalf("after Not spam = %v", listed)
	}
	if again := markSpam(t, api, admin, false, id); again.Updated != 0 {
		t.Fatalf("Not spam twice changed %d", again.Updated)
	}
}

// A sender marked as spam in one workspace is that workspace's only: the same address writing to another
// workspace reaches its inbox, and no workspace can mark or list another's conversations.
func TestSpamStaysInsideItsWorkspace(t *testing.T) {
	api, service := testAPI(t)
	alpha := createSession(t, api, service, "Spam Alpha", "admin@spam.example.com")
	beta := createSession(t, api, service, "Spam Beta", "admin@spam-beta.example.com")
	alphaSecret := connectEmail(t, api, alpha, "support@spam.example.com")
	betaSecret := connectEmail(t, api, beta, "support@spam-beta.example.com")
	alphaID := *inboundOK(t, api, alphaSecret, spamMail("pills@sender.example", "iso-a1", "Pills")).ConversationID
	betaID := *inboundOK(t, api, betaSecret, spamMail("pills@sender.example", "iso-b1", "Pills")).ConversationID

	markSpam(t, api, alpha, true, alphaID)
	next := *inboundOK(t, api, betaSecret, spamMail("pills@sender.example", "iso-b2", "Pills again")).ConversationID
	if listed := listedStatuses(t, api, beta, ""); listed[betaID] != "open" || listed[next] != "open" {
		t.Fatalf("workspace B is moved by A's spam: %v", listed)
	}
	if listed := listedStatuses(t, api, beta, "status=spam"); len(listed) != 0 {
		t.Fatalf("workspace B's Spam view = %v", listed)
	}

	for _, ids := range [][]string{{betaID}, {alphaID, betaID}} {
		for _, spam := range []bool{true, false} {
			response := request(t, api, http.MethodPut, spamPath, map[string]any{"ids": ids, "spam": spam}, alpha, nil)
			if response.Code != http.StatusNotFound {
				t.Fatalf("A marks B's conversation spam=%v: %d %s", spam, response.Code, response.Body.String())
			}
		}
	}
	if listed := listedStatuses(t, api, beta, ""); listed[betaID] != "open" {
		t.Fatalf("B's conversation changed: %v", listed)
	}
	if listed := listedStatuses(t, api, alpha, "status=spam"); listed[alphaID] != "spam" {
		t.Fatalf("a refused call undid A's spam: %v", listed)
	}

	for _, body := range []map[string]any{{"ids": []string{}, "spam": true}, {"ids": []string{alphaID, alphaID}, "spam": true}, {"ids": []string{alphaID}}, {"ids": []string{"not-an-id"}, "spam": true}} {
		if response := request(t, api, http.MethodPut, spamPath, body, alpha, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("POST %v = %d %s", body, response.Code, response.Body.String())
		}
	}
}
