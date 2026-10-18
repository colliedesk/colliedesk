package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

type namedMessage struct {
	Body       string  `json:"body"`
	AuthorName *string `json:"author_name"`
}

func authorNames(t *testing.T, api http.Handler, token, conversation string) map[string]*string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list messages status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []namedMessage `json:"items"`
	}
	decode(t, response, &list)
	names := map[string]*string{}
	for _, item := range list.Items {
		names[item.Body] = item.AuthorName
	}
	return names
}

func wantName(t *testing.T, names map[string]*string, body, want string) {
	t.Helper()
	got, ok := names[body]
	if !ok {
		t.Fatalf("message %q not in the timeline", body)
	}
	if got == nil || *got != want {
		t.Fatalf("message %q: author_name=%v, want %q", body, deref(got), want)
	}
}

func deref(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func TestTeamMessagesNameTheTeamMemberWhoWroteThem(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	ctx := context.Background()
	admin := createSession(t, api, service, "Byline Desk", "ada@byline.example.com")
	if _, err := pool.Exec(ctx, `UPDATE agents SET display_name='Ada Admin' WHERE email='ada@byline.example.com'`); err != nil {
		t.Fatal(err)
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@byline.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Who answered"})
	path := "/api/v1/conversations/" + conversation + "/messages"
	create(t, api, admin, path, map[string]any{"kind": "outgoing", "body": "reply by a person"})
	create(t, api, admin, path, map[string]any{"kind": "note", "body": "note by a person"})
	bot := issueToken(t, api, admin, "Support bot", "send")
	create(t, api, bot.Token, path, map[string]any{"kind": "note", "body": "note by a token"})
	// An imported message keeps the name it was imported with, even when it is linked to an agent.
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,author_name,kind,body)
		SELECT workspace_id,id,(SELECT id FROM agents WHERE email='ada@byline.example.com'),'Ada (Freshdesk)','note','imported note'
		FROM conversations WHERE id=$1`, conversation); err != nil {
		t.Fatal(err)
	}

	names := authorNames(t, api, admin, conversation)
	wantName(t, names, "reply by a person", "Ada Admin")
	wantName(t, names, "note by a person", "Ada Admin")
	wantName(t, names, "note by a token", "Ada Admin")
	wantName(t, names, "imported note", "Ada (Freshdesk)")

	if _, err := pool.Exec(ctx, `UPDATE agents SET display_name='' WHERE email='ada@byline.example.com'`); err != nil {
		t.Fatal(err)
	}
	names = authorNames(t, api, admin, conversation)
	wantName(t, names, "reply by a person", "ada@byline.example.com")
	wantName(t, names, "imported note", "Ada (Freshdesk)")

	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id=$1 AND author_name IS NOT NULL`, conversation).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("the name must be resolved on read, not stored: %d messages have a stored author_name, want 1", stored)
	}
}

func TestMessageAuthorNameComesFromTheMessagesOwnWorkspace(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	ctx := context.Background()
	// The same person is an agent of two workspaces, under a different name in each.
	first := createSession(t, api, service, "Name Desk One", "sam@twodesks.example.com")
	second := createSession(t, api, service, "Name Desk Two", "sam@twodesks.example.com")
	if _, err := pool.Exec(ctx, `UPDATE agents a SET display_name=CASE w.name WHEN 'Name Desk One' THEN 'Sam One' ELSE 'Sam Two' END
		FROM workspaces w WHERE w.id=a.workspace_id AND a.email='sam@twodesks.example.com'`); err != nil {
		t.Fatal(err)
	}
	conversationIn := func(token string) string {
		contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@twodesks.example.com"})
		conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Same person"})
		create(t, api, token, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "answer"})
		return conversation
	}
	one, two := conversationIn(first), conversationIn(second)

	wantName(t, authorNames(t, api, first, one), "answer", "Sam One")
	wantName(t, authorNames(t, api, second, two), "answer", "Sam Two")
	if response := request(t, api, http.MethodGet, "/api/v1/conversations/"+one+"/messages", nil, second, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace read the messages: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIncomingMessageNamesTheContactOnlyWhenTheContactSentIt(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Sender Desk", "admin@sender.example.com")
	secret := connectEmail(t, api, admin, "help@sender.example.com")
	first := inboundOK(t, api, secret, mail([]string{
		"From: Casey Customer <Casey@Customer.example>", "To: help@sender.example.com",
		"Subject: Broken invoice", "Message-ID: <invoice-1@customer.example>",
	}, "from the contact"))
	conversation := *first.ConversationID
	// A colleague copied on the thread answers it; the conversation's contact did not write this.
	inboundOK(t, api, secret, mail([]string{
		"From: Robin Colleague <robin@customer.example>", "To: help@sender.example.com",
		"Subject: Re: Broken invoice", "Message-ID: <invoice-2@customer.example>",
		"In-Reply-To: <invoice-1@customer.example>",
	}, "from a colleague"))

	names := authorNames(t, api, admin, conversation)
	wantName(t, names, "from the contact", "Casey Customer")
	if got := names["from a colleague"]; got != nil {
		t.Fatalf("a message from someone else must not carry the contact's name: author_name=%q", *got)
	}

	if _, err := pool.Exec(context.Background(), `UPDATE contacts SET name='Casey C.' WHERE email='casey@customer.example'`); err != nil {
		t.Fatal(err)
	}
	wantName(t, authorNames(t, api, admin, conversation), "from the contact", "Casey C.")
}
