package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type listedPreview struct {
	ID                    string `json:"id"`
	LatestCustomerMessage *struct {
		Excerpt   string    `json:"excerpt"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"latest_customer_message"`
}

func listedPreviews(t *testing.T, api http.Handler, token string) map[string]listedPreview {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?status=open,pending,resolved,closed", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []listedPreview `json:"items"`
	}
	decode(t, response, &page)
	byID := map[string]listedPreview{}
	for _, item := range page.Items {
		byID[item.ID] = item
	}
	return byID
}

// A row of the inbox says what the customer last wrote and when: the team's own replies and notes never
// stand in for it, a long message is cut to one line, and a conversation the team started has none yet.
func TestConversationListCarriesTheCustomersLatestWords(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Preview Desk", "admin@preview.example.com")
	secret := connectEmail(t, api, admin, "support@preview.example.com")

	first := inboundOK(t, api, secret, mail([]string{"From: Hana Sato <hana@kumo.example>", "To: support@preview.example.com",
		"Subject: Refund still not on my card", "Message-ID: <refund-1@kumo.example>", "Date: Mon, 02 Nov 2026 09:02:00 +0000"},
		"You confirmed the refund ten days ago."))
	conversation := *first.ConversationID
	long := "Still nothing on my card.\n\nCan someone check with the bank? " + strings.Repeat("I am starting to worry. ", 20)
	inboundOK(t, api, secret, mail([]string{"From: Hana Sato <hana@kumo.example>", "To: support@preview.example.com",
		"Subject: Re: Refund still not on my card", "Message-ID: <refund-2@kumo.example>", "In-Reply-To: <refund-1@kumo.example>",
		"Date: Tue, 03 Nov 2026 09:02:00 +0000"}, long))
	for _, kind := range []string{"note", "outgoing"} {
		if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
			map[string]any{"kind": kind, "body": "Our own words, " + kind}, admin, nil); response.Code != http.StatusCreated {
			t.Fatalf("post %s: %d %s", kind, response.Code, response.Body.String())
		}
	}

	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Jonas Berg", "email": "jonas@berg.example"})
	started := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "We started this one"}, admin, nil)
	if started.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", started.Code, started.Body.String())
	}
	var ours struct {
		ID string `json:"id"`
	}
	decode(t, started, &ours)

	rows := listedPreviews(t, api, admin)
	latest := rows[conversation].LatestCustomerMessage
	if latest == nil {
		t.Fatalf("the customer's conversation has no latest message: %+v", rows[conversation])
	}
	want := "Still nothing on my card. Can someone check with the bank? I am starting to worry. I am starting to worry."
	if !strings.HasPrefix(latest.Excerpt, want) || !strings.HasSuffix(latest.Excerpt, "…") || len([]rune(latest.Excerpt)) > 141 {
		t.Fatalf("excerpt = %q (%d runes), want one line starting %q, cut with …", latest.Excerpt, len([]rune(latest.Excerpt)), want)
	}
	var timeline struct {
		Items []struct {
			Kind      string    `json:"kind"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, admin, nil), &timeline)
	var secondMail time.Time
	for _, item := range timeline.Items {
		if item.Kind == "incoming" {
			secondMail = item.CreatedAt
		}
	}
	if secondMail.IsZero() || !latest.CreatedAt.Equal(secondMail) {
		t.Fatalf("created_at = %s, want the second mail's %s, not a time of our note or reply", latest.CreatedAt, secondMail)
	}
	if rows[ours.ID].LatestCustomerMessage != nil {
		t.Fatalf("a conversation the team started has a customer message: %+v", rows[ours.ID].LatestCustomerMessage)
	}
}
