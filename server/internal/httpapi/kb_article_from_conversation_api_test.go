package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type conversationArticleView struct {
	gapDraftView
	GapSource *struct {
		GapID         string `json:"gap_id"`
		Requested     bool   `json:"requested"`
		Conversations []struct {
			ConversationID string `json:"conversation_id"`
		} `json:"conversations"`
	} `json:"gap_source"`
	ReplySource *struct {
		ConversationID string `json:"conversation_id"`
		DisplayRef     string `json:"display_ref"`
		Subject        string `json:"subject"`
		MessageID      string `json:"message_id"`
		Question       string `json:"question"`
	} `json:"reply_source"`
}

func (v conversationArticleView) sourceIDs() string {
	ids := []string{}
	if v.GapSource != nil {
		for _, c := range v.GapSource.Conversations {
			ids = append(ids, c.ConversationID)
		}
	}
	return strings.Join(ids, ",")
}

func TestStartingADraftFromARequestOpensItBesideTheConversationsThatAsked(t *testing.T) {
	d := newDraftDesk(t)
	first, second := d.conversationA, d.conversation(t, "Locked out")
	gap := askForArticle(t, d, d.adminA, first, map[string]any{"title": "Reset a password"}, http.StatusCreated).Gap.ID
	askForArticle(t, d, d.adminA, second, map[string]any{"gap_id": gap}, http.StatusCreated)
	calls := len(d.fake.Requests())
	path := "/api/v1/kb/gaps/" + gap + "/article"

	response := postWithKey(t, d.api, d.adminA, path, "start-1", map[string]any{})
	validateContractResponse(t, newRequest(t, http.MethodPost, path, map[string]any{}, d.adminA, nil), response)
	var article conversationArticleView
	decode(t, response, &article)
	if response.Code != http.StatusCreated || article.Status != "draft" || article.Title != "Reset a password" ||
		article.CollectionID != queryString(t, d.pool, `SELECT collection_id::text FROM kb_articles WHERE id=$1`, d.articleA) {
		t.Fatalf("start = %d %+v, want a draft titled Reset a password in the first collection", response.Code, article.gapDraftView)
	}
	if article.GapSource == nil || article.GapSource.GapID != gap || !article.GapSource.Requested || article.sourceIDs() != second+","+first {
		t.Fatalf("gap source = %+v, want the two conversations that asked, newest first", article.GapSource)
	}
	if len(d.fake.Requests()) != calls {
		t.Fatalf("starting a draft called the model")
	}
	var again conversationArticleView
	decode(t, postWithKey(t, d.api, d.adminA, path, "start-1", map[string]any{}), &again)
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, d.adminA, http.StatusOK, &again)
	if again.ID != article.ID || again.sourceIDs() != article.sourceIDs() || !again.GapSource.Requested {
		t.Fatalf("again = %+v, want the same draft and sources", again)
	}

	writer := customToken(t, d.api, d.adminA, "KB writer", "kb_articles:read", "kb_articles:create")
	var blind conversationArticleView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, writer, http.StatusOK, &blind)
	if blind.GapSource == nil || blind.sourceIDs() != "" {
		t.Fatalf("writer reads %+v, want the gap without its conversations", blind.GapSource)
	}
	refused(t, d.api, http.MethodPost, path, map[string]any{}, customToken(t, d.api, d.adminA, "KB reader", "kb_articles:read"), "kb_articles:create")
	kbCall(t, d.api, http.MethodPost, path, map[string]any{}, d.adminB, http.StatusNotFound, nil)
}

func TestAnArticleMadeFromASentReplyHasNoNamesEmailsOrLongNumbers(t *testing.T) {
	d := newDraftDesk(t)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Ann Lee"}, d.adminA, http.StatusOK, nil)
	if _, err := d.pool.Exec(context.Background(), `UPDATE contacts SET organization='Lovelace Engines' WHERE id=$1`, d.contactA); err != nil {
		t.Fatal(err)
	}
	conversation := d.conversation(t, "Ada cannot print order 12345678")
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
		VALUES($1,$2,'incoming','Hi, Ada Lovelace from Lovelace Engines here. Order 98765432 will not print, mail ada@customer-a.example',now()-interval '1 hour')`,
		d.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	var reply, note struct {
		ID string `json:"id"`
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing",
		"body": "Hi Ada, Ann Lee here. Open tray two and pull the paper out. Still stuck? Write to help@desk.example or call 0912345678."}, d.adminA, http.StatusCreated, &reply)
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "internal"}, d.adminA, http.StatusCreated, &note)
	path := "/api/v1/conversations/" + conversation + "/messages/" + reply.ID + "/article"

	response := postWithKey(t, d.api, d.adminA, path, "reply-1", map[string]any{})
	validateContractResponse(t, newRequest(t, http.MethodPost, path, map[string]any{}, d.adminA, nil), response)
	var article conversationArticleView
	decode(t, response, &article)
	if response.Code != http.StatusCreated || article.Status != "draft" || !strings.Contains(article.BodyMD, "Open tray two and pull the paper out") {
		t.Fatalf("article = %d %+v, want a draft holding the reply", response.Code, article.gapDraftView)
	}
	source := article.ReplySource
	if source == nil || source.ConversationID != conversation || source.MessageID != reply.ID || source.DisplayRef == "" ||
		!strings.Contains(source.Question, "will not print") {
		t.Fatalf("reply source = %+v, want the conversation, the reply and the customer question", source)
	}
	text := article.Title + "\n" + article.BodyMD + "\n" + source.Subject + "\n" + source.Question
	if found := anEmail.FindString(text); found != "" {
		t.Errorf("the email %q reached the draft", found)
	}
	if found := sixOrMoreRun.FindString(text); found != "" {
		t.Errorf("the number %q reached the draft", found)
	}
	for _, name := range []string{"Ada", "Lovelace", "Ann Lee"} {
		if strings.Contains(text, name) {
			t.Errorf("the name %q reached the draft: %s", name, text)
		}
	}
	var stored conversationArticleView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, d.adminA, http.StatusOK, &stored)
	if stored.ReplySource == nil || stored.ReplySource.Question != source.Question {
		t.Fatalf("read again = %+v, want the same reply source", stored.ReplySource)
	}
	writer := customToken(t, d.api, d.adminA, "KB writer", "kb_articles:read", "kb_articles:create")
	refused(t, d.api, http.MethodPost, path, map[string]any{}, writer, "conversations:read")
	var hidden conversationArticleView
	kbCall(t, d.api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, writer, http.StatusOK, &hidden)
	if hidden.ReplySource != nil {
		t.Fatalf("writer reads %+v, want no conversation shown", hidden.ReplySource)
	}

	notePath := "/api/v1/conversations/" + conversation + "/messages/" + note.ID + "/article"
	kbCall(t, d.api, http.MethodPost, notePath, map[string]any{}, d.adminA, http.StatusBadRequest, nil)
	kbCall(t, d.api, http.MethodPost, path, map[string]any{}, d.adminB, http.StatusNotFound, nil)
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+d.conversationA+"/messages/"+reply.ID+"/article", map[string]any{}, d.adminA, http.StatusNotFound, nil)
}
