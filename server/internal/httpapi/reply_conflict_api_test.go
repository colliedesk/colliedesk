package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type replyConflict struct {
	Error struct {
		Code              string `json:"code"`
		Message           string `json:"message"`
		LatestReplyNumber int    `json:"latest_reply_number"`
		NewerReplies      []struct {
			ID          string `json:"id"`
			Body        string `json:"body"`
			ReplyNumber int    `json:"reply_number"`
			CreatedAt   string `json:"created_at"`
			AuthorName  string `json:"author_name"`
			Origin      *struct {
				System string `json:"system"`
			} `json:"origin"`
		} `json:"newer_replies"`
	} `json:"error"`
}

type numberedMessage struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	ReplyNumber *int   `json:"reply_number"`
}

// replyDesk is a workspace with an admin, a teammate and one conversation, and a second workspace.
type replyDesk struct {
	api                    http.Handler
	admin, teammate, other string
	conversation           string
	otherConversation      string
	workspace              string
	pool                   *pgxpool.Pool
}

func newReplyDesk(t *testing.T) (replyDesk, func(query string, args ...any) int) {
	t.Helper()
	api, service, pool := testAPIWithPool(t)
	d := replyDesk{api: api, pool: pool}
	d.admin = createSession(t, api, service, "Reply Desk", "admin@reply.example.com")
	d.teammate = joinAsAgent(t, api, d.admin, "lan@reply.example.com")
	contact := create(t, api, d.admin, "/api/v1/contacts", map[string]any{"name": "Customer", "email": "customer@reply.example.com"})
	d.conversation = create(t, api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Refund"})
	d.workspace = workspaceID(t, api, d.admin)
	d.other = createSession(t, api, service, "Other Reply Desk", "admin@other-reply.example.com")
	otherContact := create(t, api, d.other, "/api/v1/contacts", map[string]any{"name": "Other", "email": "other@reply.example.com"})
	d.otherConversation = create(t, api, d.other, "/api/v1/conversations", map[string]any{"contact_id": otherContact, "subject": "Other"})
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return d, count
}

func (d replyDesk) send(t *testing.T, token, conversation string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", body, token, nil)
}

func latestReplyNumber(t *testing.T, api http.Handler, token, conversation string) int {
	t.Helper()
	var detail struct {
		LatestReplyNumber *int `json:"latest_reply_number"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, token, nil), &detail)
	if detail.LatestReplyNumber == nil {
		t.Fatal("conversation has no latest_reply_number")
	}
	return *detail.LatestReplyNumber
}

func TestReplyConflictWhenSomeoneRepliedSinceTheSenderStartedWriting(t *testing.T) {
	d, count := newReplyDesk(t)
	if got := latestReplyNumber(t, d.api, d.admin, d.conversation); got != 0 {
		t.Fatalf("a new conversation has latest_reply_number %d", got)
	}
	// Lan answers first, having seen no reply.
	first := d.send(t, d.teammate, d.conversation, map[string]any{"kind": "outgoing", "body": "Your refund is on its way.", "seen_reply_number": 0})
	if first.Code != http.StatusCreated {
		t.Fatalf("first reply status=%d body=%s", first.Code, first.Body.String())
	}
	var lan numberedMessage
	decode(t, first, &lan)
	if lan.ReplyNumber == nil || *lan.ReplyNumber != 1 {
		t.Fatalf("first reply number = %v", lan.ReplyNumber)
	}
	// The admin started writing before it arrived: told, with Lan's reply, and nothing is stored.
	before, queuedBefore := count(`SELECT count(*) FROM messages`), count(`SELECT count(*) FROM email_outbox`)
	refused := d.send(t, d.admin, d.conversation, map[string]any{"kind": "outgoing", "body": "We will refund you.", "seen_reply_number": 0})
	if refused.Code != http.StatusConflict {
		t.Fatalf("stale reply status=%d body=%s", refused.Code, refused.Body.String())
	}
	var conflict replyConflict
	decode(t, refused, &conflict)
	if conflict.Error.Code != "reply_conflict" || conflict.Error.LatestReplyNumber != 1 || len(conflict.Error.NewerReplies) != 1 ||
		conflict.Error.NewerReplies[0].ID != lan.ID || conflict.Error.NewerReplies[0].Body != "Your refund is on its way." || conflict.Error.NewerReplies[0].ReplyNumber != 1 {
		t.Fatalf("conflict = %+v", conflict.Error)
	}
	if after := count(`SELECT count(*) FROM messages`); after != before {
		t.Fatalf("a refused reply stored %d messages", after-before)
	}
	if queued := count(`SELECT count(*) FROM email_outbox`); queued != queuedBefore {
		t.Fatalf("a refused reply queued %d emails", queued-queuedBefore)
	}
	// A private note never waits on that check.
	if note := d.send(t, d.admin, d.conversation, map[string]any{"kind": "note", "body": "Checking", "seen_reply_number": 0}); note.Code != http.StatusCreated {
		t.Fatalf("note status=%d", note.Code)
	}
	// Sent anyway, having read it: the mark moves to the latest number and the reply goes.
	second := d.send(t, d.admin, d.conversation, map[string]any{"kind": "outgoing", "body": "We will refund you.", "seen_reply_number": 1})
	var admin numberedMessage
	decode(t, second, &admin)
	if second.Code != http.StatusCreated || admin.ReplyNumber == nil || *admin.ReplyNumber != 2 {
		t.Fatalf("reply with the latest mark status=%d number=%v", second.Code, admin.ReplyNumber)
	}
	// A client that sends no mark (an older one, or the API) is not checked.
	if old := d.send(t, d.admin, d.conversation, map[string]any{"kind": "outgoing", "body": "One more"}); old.Code != http.StatusCreated {
		t.Fatalf("reply without a mark status=%d", old.Code)
	}
	if got := latestReplyNumber(t, d.api, d.admin, d.conversation); got != 3 {
		t.Fatalf("latest_reply_number = %d, want 3", got)
	}
	var timeline struct {
		Items []numberedMessage `json:"items"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+d.conversation+"/messages", nil, d.admin, nil), &timeline)
	numbers := ""
	for _, item := range timeline.Items {
		if item.ReplyNumber == nil {
			numbers += item.Kind + ":- "
		} else {
			numbers += fmt.Sprintf("%s:%d ", item.Kind, *item.ReplyNumber)
		}
	}
	if numbers != "outgoing:1 note:- outgoing:2 outgoing:3 " {
		t.Fatalf("timeline numbers = %s", numbers)
	}
	for _, bad := range []any{-1, "1", 1.5} {
		if response := d.send(t, d.admin, d.conversation, map[string]any{"kind": "outgoing", "body": "x", "seen_reply_number": bad}); response.Code != http.StatusBadRequest {
			t.Fatalf("seen_reply_number %v status=%d", bad, response.Code)
		}
	}
	// Another workspace's conversation is not found, whatever the mark.
	if response := d.send(t, d.admin, d.otherConversation, map[string]any{"kind": "outgoing", "body": "x", "seen_reply_number": 0}); response.Code != http.StatusNotFound {
		t.Fatalf("other workspace status=%d", response.Code)
	}
}

func TestTwoRepliesSentAtOnceWithTheSameMarkOnlyOneGoes(t *testing.T) {
	d, count := newReplyDesk(t)
	for round := range 5 {
		seen := latestReplyNumber(t, d.api, d.admin, d.conversation)
		codes := make([]int, 2)
		var wait sync.WaitGroup
		for index, token := range []string{d.admin, d.teammate} {
			wait.Add(1)
			go func() {
				defer wait.Done()
				codes[index] = d.send(t, token, d.conversation, map[string]any{"kind": "outgoing", "body": fmt.Sprintf("round %d by %d", round, index), "seen_reply_number": seen}).Code
			}()
		}
		wait.Wait()
		if !(codes[0] == http.StatusCreated && codes[1] == http.StatusConflict) && !(codes[0] == http.StatusConflict && codes[1] == http.StatusCreated) {
			t.Fatalf("round %d codes = %v, want one 201 and one 409", round, codes)
		}
	}
	if replies := count(`SELECT count(*) FROM messages WHERE kind='outgoing'`); replies != 5 {
		t.Fatalf("replies stored = %d, want 5", replies)
	}
}

func TestAReplyWithFilesAndAStaleMarkStoresNoFile(t *testing.T) {
	d, count := newReplyDesk(t)
	if response := d.send(t, d.teammate, d.conversation, map[string]any{"kind": "outgoing", "body": "First"}); response.Code != http.StatusCreated {
		t.Fatalf("first reply status=%d", response.Code)
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range [][2]string{{"kind", "outgoing"}, {"body", "With a file"}, {"seen_reply_number", "0"}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("files", "invoice.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("invoice")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+d.conversation+"/messages", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+d.admin)
	response := serve(d.api, req)
	var conflict replyConflict
	decode(t, response, &conflict)
	if response.Code != http.StatusConflict || conflict.Error.Code != "reply_conflict" || len(conflict.Error.NewerReplies) != 1 {
		t.Fatalf("multipart stale reply status=%d body=%s", response.Code, response.Body.String())
	}
	if files := count(`SELECT count(*) FROM attachments`); files != 0 {
		t.Fatalf("attachments stored = %d", files)
	}
	if replies := count(`SELECT count(*) FROM messages WHERE kind='outgoing'`); replies != 1 {
		t.Fatalf("replies = %d", replies)
	}
}

// A reply imported later (a Freshdesk import run again) gets the next number though its time is old:
// the conflict lists it by time with where it came from, so nobody thinks it was just written.
func TestReplyConflictListsAnImportedReplyByItsOwnTimeAndOrigin(t *testing.T) {
	d, count := newReplyDesk(t)
	if response := d.send(t, d.teammate, d.conversation, map[string]any{"kind": "outgoing", "body": "Native reply"}); response.Code != http.StatusCreated {
		t.Fatalf("native reply status=%d", response.Code)
	}
	_ = count(`WITH c AS (SELECT workspace_id,id FROM conversations WHERE id=$1)
		INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at,origin)
		SELECT workspace_id,id,'outgoing','Imported old reply','2025-01-02T03:04:05Z','{"system":"freshdesk","external_id":"77","kind":"reply","from":null,"to":[],"cc":[],"bcc":[]}' FROM c RETURNING 1`, d.conversation)
	response := d.send(t, d.admin, d.conversation, map[string]any{"kind": "outgoing", "body": "Late", "seen_reply_number": 0})
	var conflict replyConflict
	decode(t, response, &conflict)
	if response.Code != http.StatusConflict || len(conflict.Error.NewerReplies) != 2 || conflict.Error.LatestReplyNumber != 2 {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
	imported, native := conflict.Error.NewerReplies[0], conflict.Error.NewerReplies[1]
	if imported.Body != "Imported old reply" || imported.ReplyNumber != 2 || imported.Origin == nil || imported.Origin.System != "freshdesk" || native.Body != "Native reply" {
		t.Fatalf("newer replies by time = %+v", conflict.Error.NewerReplies)
	}
}

// A reply locks its conversation to number it while a bulk change locks many in id order: they wait for
// each other, never deadlock (a deadlock answers 409 conflict).
func TestRepliesAndABulkChangeOnTheSameConversationsNeverDeadlock(t *testing.T) {
	d, _ := newReplyDesk(t)
	contact := create(t, d.api, d.admin, "/api/v1/contacts", map[string]any{"name": "Bulk", "email": "bulk@reply.example.com"})
	ids := []string{d.conversation}
	for index := range 19 {
		ids = append(ids, create(t, d.api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": fmt.Sprint("Bulk ", index)}))
	}
	for round := range 3 {
		var wait sync.WaitGroup
		failures := make(chan string, len(ids)+1)
		wait.Add(1)
		go func() {
			defer wait.Done()
			status := []string{"pending", "open"}[round%2]
			if response := request(t, d.api, http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": ids, "changes": map[string]any{"status": status}}, d.admin, nil); response.Code != http.StatusOK {
				failures <- fmt.Sprintf("bulk status=%d body=%s", response.Code, response.Body.String())
			}
		}()
		for index := len(ids) - 1; index >= 0; index-- {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if response := d.send(t, d.teammate, ids[index], map[string]any{"kind": "outgoing", "body": "Racing", "seen_reply_number": round}); response.Code != http.StatusCreated {
					failures <- fmt.Sprintf("reply status=%d body=%s", response.Code, response.Body.String())
				}
			}()
		}
		wait.Wait()
		close(failures)
		for failure := range failures {
			t.Fatalf("round %d: %s", round, failure)
		}
	}
}

// The person who approves an AI agent's reply draft says which replies they had on screen: a reply sent
// since refuses the approval and the draft keeps waiting, so it can be read again and approved.
func TestApprovingAReplyDraftAfterSomeoneRepliedKeepsTheDraftWaiting(t *testing.T) {
	d, _ := newReplyDesk(t)
	token := issueToken(t, d.api, d.admin, "Claude Code", "draft")
	filed := d.send(t, token.Token, d.conversation, map[string]any{"kind": "outgoing", "body": "Drafted by the agent"})
	var accepted struct {
		RequestID string `json:"request_id"`
	}
	decode(t, filed, &accepted)
	if filed.Code != http.StatusAccepted || accepted.RequestID == "" {
		t.Fatalf("draft status=%d body=%s", filed.Code, filed.Body.String())
	}
	if response := d.send(t, d.teammate, d.conversation, map[string]any{"kind": "outgoing", "body": "Lan answered first"}); response.Code != http.StatusCreated {
		t.Fatalf("teammate reply status=%d", response.Code)
	}
	approve := func(body map[string]any) *httptest.ResponseRecorder {
		return request(t, d.api, http.MethodPost, "/api/v1/agent-requests/"+accepted.RequestID+"/approve", body, d.admin, nil)
	}
	stale := approve(map[string]any{"seen_reply_number": 0})
	var conflict replyConflict
	decode(t, stale, &conflict)
	if stale.Code != http.StatusConflict || conflict.Error.Code != "reply_conflict" || len(conflict.Error.NewerReplies) != 1 || conflict.Error.NewerReplies[0].Body != "Lan answered first" {
		t.Fatalf("stale approval status=%d body=%s", stale.Code, stale.Body.String())
	}
	var waiting struct {
		Status string `json:"status"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/agent-requests/"+accepted.RequestID, nil, d.admin, nil), &waiting)
	if waiting.Status != "pending" {
		t.Fatalf("draft after a refused approval is %q, want pending", waiting.Status)
	}
	approved := approve(map[string]any{"seen_reply_number": 1})
	var result struct {
		Request struct {
			Status       string `json:"status"`
			ResultStatus *int   `json:"result_status"`
		} `json:"request"`
		Result numberedMessage `json:"result"`
	}
	decode(t, approved, &result)
	if approved.Code != http.StatusOK || result.Request.Status != "approved" || result.Result.ReplyNumber == nil || *result.Result.ReplyNumber != 2 {
		t.Fatalf("approval with the latest mark status=%d body=%s", approved.Code, approved.Body.String())
	}
	// The mark is for reply drafts only.
	other := request(t, d.api, http.MethodPatch, "/api/v1/conversations/"+d.conversation, map[string]any{"priority": "high"}, issueToken(t, d.api, d.admin, "Reader", "read").Token, nil)
	var otherRequest struct {
		RequestID string `json:"request_id"`
	}
	decode(t, other, &otherRequest)
	if response := request(t, d.api, http.MethodPost, "/api/v1/agent-requests/"+otherRequest.RequestID+"/approve", map[string]any{"seen_reply_number": 2}, d.admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("seen on a non-draft approval status=%d", response.Code)
	}
}

// An importer writes a reply in its own transaction (the numbering lock, then the conversation row for
// last_public) while a person replies (the same locks in the same order): both are stored, numbered
// one after the other, with no deadlock and no number twice.
func TestAnImportedReplyAndAPersonsReplyAtOnceAreNumberedInTurn(t *testing.T) {
	d, count := newReplyDesk(t)
	for round := range 10 {
		var wait sync.WaitGroup
		var importErr error
		var status int
		wait.Add(2)
		go func() {
			defer wait.Done()
			_, importErr = d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
				SELECT workspace_id,id,'outgoing','imported',now()-interval '1 day' FROM conversations WHERE id=$1`, d.conversation)
		}()
		go func() {
			defer wait.Done()
			status = d.send(t, d.teammate, d.conversation, map[string]any{"kind": "outgoing", "body": fmt.Sprint("person ", round)}).Code
		}()
		wait.Wait()
		if importErr != nil || status != http.StatusCreated {
			t.Fatalf("round %d: import error %v, reply status %d", round, importErr, status)
		}
	}
	if got := count(`SELECT count(DISTINCT reply_number)*1000+max(reply_number) FROM messages WHERE conversation_id=$1`, d.conversation); got != 20*1000+20 {
		t.Fatalf("numbers = %d, want 20 distinct up to 20", got)
	}
}
