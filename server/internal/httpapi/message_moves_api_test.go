package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A chat message taken back to the intake queue is out of its conversation for AI as for people: a draft
// never reads it.
func TestADraftLeavesOutAMessageTakenBackToTheQueue(t *testing.T) {
	d := newDraftDesk(t)
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,detached_at) VALUES($1,$2,'incoming','DETACHED-CANARY other matter',now())`,
		d.workspaceA, d.conversationA); err != nil {
		t.Fatal(err)
	}
	d.fake.Script(draftAnswer("Hi Ada.", nil, ""))
	d.ask(t, d.adminA, d.conversationA, "")
	d.run(t)
	requests := d.draftRequests()
	if len(requests) != 1 {
		t.Fatalf("provider calls = %d", len(requests))
	}
	if prompt := requests[0].System + requests[0].Messages[0].Text; strings.Contains(prompt, "DETACHED-CANARY") || !strings.Contains(prompt, "My printer jams") {
		t.Fatalf("prompt:\n%s", prompt)
	}
}

// moveDesk is a workspace with email whose customers wrote in, for moving their messages.
type moveDesk struct {
	api    http.Handler
	admin  string
	secret string
}

func newMoveDesk(t *testing.T, name string) moveDesk {
	t.Helper()
	api, service := testAPI(t)
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	admin := createSession(t, api, service, name, "admin@"+slug+".example.com")
	return moveDesk{api: api, admin: admin, secret: connectEmail(t, api, admin, "support@"+slug+".example.com")}
}

// mail sends a customer's mail (its header lines, then a word its body names) and returns its
// conversation and message.
func (d moveDesk) mail(t *testing.T, lines ...string) (string, string) {
	t.Helper()
	result := inboundOK(t, d.api, d.secret, mail(lines[:len(lines)-1], "Body of "+lines[len(lines)-1]))
	if result.ConversationID == nil || result.MessageID == nil {
		t.Fatalf("mail not stored: %+v", result)
	}
	return *result.ConversationID, *result.MessageID
}

type moveResponse struct {
	Move struct {
		ID                  string   `json:"id"`
		ToConversationID    *string  `json:"to_conversation_id"`
		CreatedConversation bool     `json:"created_conversation"`
		ClosedSource        bool     `json:"closed_source"`
		Undoable            bool     `json:"undoable"`
		MessageIDs          []string `json:"message_ids"`
	} `json:"move"`
	Warnings []string `json:"warnings"`
}

func (d moveDesk) move(t *testing.T, token, from string, body map[string]any, want int) moveResponse {
	t.Helper()
	response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+from+"/messages/move", body, token, nil)
	if response.Code != want {
		t.Fatalf("move %v: %d %s", body, response.Code, response.Body.String())
	}
	var moved moveResponse
	if want == http.StatusOK {
		decode(t, response, &moved)
	}
	return moved
}

func (d moveDesk) status(t *testing.T, conversation string) string {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, d.admin, nil)
	var c struct {
		Status string `json:"status"`
	}
	decode(t, response, &c)
	return c.Status
}

func bodies(items []timelineMessage) string {
	var out []string
	for _, item := range items {
		out = append(out, item.Kind+":"+item.Body)
	}
	return strings.Join(out, "|")
}

// A person moves a customer message to another conversation: it leaves with its files, the team's notes
// stay, a resolved conversation that gets it reopens, the customer's reply to it lands where it went,
// and undo puts it and the status back; undoing twice changes nothing, and only the newest move of a
// message is undone.
func TestMovingAMessageAndUndoingItPutsItBack(t *testing.T) {
	d := newMoveDesk(t, "Move Desk")
	printer, first := d.mail(t, "From: kim@customer.example", "Subject: Printer", "Message-ID: <printer-1@customer.example>", "Printer")
	invoice, _ := d.mail(t, "From: kim@customer.example", "Subject: Invoice", "Message-ID: <invoice-1@customer.example>", "Invoice")
	if response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+printer+"/messages", map[string]any{"kind": "note", "body": "Looking"}, d.admin, nil); response.Code != http.StatusCreated {
		t.Fatalf("note: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodPatch, "/api/v1/conversations/"+invoice, map[string]any{"status": "resolved"}, d.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", response.Code, response.Body.String())
	}
	note := timeline(t, d.api, d.admin, printer)[1].ID
	d.move(t, d.admin, printer, map[string]any{"message_ids": []string{note}, "to": map[string]any{"conversation_id": invoice}}, http.StatusBadRequest)
	d.move(t, d.admin, invoice, map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": printer}}, http.StatusBadRequest)

	moved := d.move(t, d.admin, printer, map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": invoice}}, http.StatusOK)
	if len(moved.Warnings) != 0 || !moved.Move.Undoable || moved.Move.ToConversationID == nil || *moved.Move.ToConversationID != invoice {
		t.Fatalf("moved = %+v", moved)
	}
	if got := bodies(timeline(t, d.api, d.admin, printer)); got != "note:Looking" {
		t.Fatalf("source timeline = %s", got)
	}
	if got := bodies(timeline(t, d.api, d.admin, invoice)); !strings.Contains(got, "Body of Printer") || d.status(t, invoice) != "open" {
		t.Fatalf("target = %s, %s", got, d.status(t, invoice))
	}
	for _, conversation := range []string{printer, invoice} {
		response := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/message-moves", nil, d.admin, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), moved.Move.ID) {
			t.Fatalf("moves of %s: %d %s", conversation, response.Code, response.Body.String())
		}
	}
	// The customer answers the moved mail: the answer follows it.
	reply := inboundOK(t, d.api, d.secret, mail([]string{"From: kim@customer.example", "Subject: Re: Printer", "Message-ID: <printer-2@customer.example>",
		"In-Reply-To: <printer-1@customer.example>"}, "Still broken."))
	if reply.ConversationID == nil || *reply.ConversationID != invoice {
		t.Fatalf("the reply went to %v, want %s", reply.ConversationID, invoice)
	}

	undo := request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+moved.Move.ID+"/undo", nil, d.admin, nil)
	if undo.Code != http.StatusOK || !strings.Contains(undo.Body.String(), `"undone_at":"`) {
		t.Fatalf("undo: %d %s", undo.Code, undo.Body.String())
	}
	if got := bodies(timeline(t, d.api, d.admin, printer)); got != "incoming:Body of Printer|note:Looking" {
		t.Fatalf("source after undo = %s", got)
	}
	// A customer wrote in the target since: it stays open for them.
	if got := d.status(t, invoice); got != "open" {
		t.Fatalf("target after undo = %s", got)
	}
	if again := request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+moved.Move.ID+"/undo", nil, d.admin, nil); again.Code != http.StatusOK {
		t.Fatalf("undo again: %d %s", again.Code, again.Body.String())
	}
	// Moved, then moved on: the first move is no longer the one to undo.
	there := d.move(t, d.admin, printer, map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": invoice}}, http.StatusOK)
	d.move(t, d.admin, invoice, map[string]any{"message_ids": []string{first}, "to": map[string]any{"new": map[string]any{"subject": "Printer again"}}}, http.StatusOK)
	if stale := request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+there.Move.ID+"/undo", nil, d.admin, nil); stale.Code != http.StatusConflict ||
		!strings.Contains(stale.Body.String(), "move_superseded") {
		t.Fatalf("undo a superseded move: %d %s", stale.Code, stale.Body.String())
	}
}

// Undo gives a resolved target its status back while nobody wrote or changed it since.
func TestUndoGivesTheTargetItsStatusBack(t *testing.T) {
	d := newMoveDesk(t, "Status Desk")
	printer, first := d.mail(t, "From: kim@customer.example", "Subject: Printer", "Message-ID: <printer-1@customer.example>", "Printer")
	invoice, _ := d.mail(t, "From: kim@customer.example", "Subject: Invoice", "Message-ID: <invoice-1@customer.example>", "Invoice")
	request(t, d.api, http.MethodPatch, "/api/v1/conversations/"+invoice, map[string]any{"status": "resolved"}, d.admin, nil)
	moved := d.move(t, d.admin, printer, map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": invoice}}, http.StatusOK)
	request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+moved.Move.ID+"/undo", nil, d.admin, nil)
	if got := d.status(t, invoice); got != "resolved" {
		t.Fatalf("target after undo = %s", got)
	}
}

// A split opens a conversation for the messages; its undo closes it, pointing back. A conversation that
// was no new request closes into the one its messages joined, unless someone worked on it, and its undo
// reopens it. A message from another customer says so and is never threaded into this customer's mail.
func TestSplittingAndClosingASourceThatWasNoNewRequest(t *testing.T) {
	d := newMoveDesk(t, "Split Desk")
	printer, first := d.mail(t, "From: kim@customer.example", "Subject: Printer", "Message-ID: <printer-1@customer.example>", "Printer")
	_, second := d.mail(t, "From: kim@customer.example", "Subject: Re: Printer", "Message-ID: <printer-2@customer.example>", "In-Reply-To: <printer-1@customer.example>", "Scanner")
	split := d.move(t, d.admin, printer, map[string]any{"message_ids": []string{second}, "to": map[string]any{"new": map[string]any{"subject": "Scanner"}}}, http.StatusOK)
	if !split.Move.CreatedConversation || split.Move.ToConversationID == nil || len(split.Warnings) != 0 {
		t.Fatalf("split = %+v", split)
	}
	created := *split.Move.ToConversationID
	if got := bodies(timeline(t, d.api, d.admin, created)); got != "incoming:Body of Scanner" {
		t.Fatalf("split conversation = %s", got)
	}
	request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+split.Move.ID+"/undo", nil, d.admin, nil)
	if got := d.status(t, created); got != "closed" || bodies(timeline(t, d.api, d.admin, printer)) != "incoming:Body of Printer|incoming:Body of Scanner" {
		t.Fatalf("after undoing the split: %s, %s", got, bodies(timeline(t, d.api, d.admin, printer)))
	}

	lee, leeMessage := d.mail(t, "From: lee@customer.example", "Subject: Printer too", "Message-ID: <lee-1@customer.example>", "Lee")
	d.move(t, d.admin, lee, map[string]any{"message_ids": []string{leeMessage}, "to": map[string]any{"conversation_id": printer}, "close_source": "yes"}, http.StatusBadRequest)
	closed := d.move(t, d.admin, lee, map[string]any{"message_ids": []string{leeMessage}, "to": map[string]any{"conversation_id": printer}, "close_source": true}, http.StatusOK)
	if !closed.Move.ClosedSource || strings.Join(closed.Warnings, ",") != "other_contact" || d.status(t, lee) != "closed" {
		t.Fatalf("close source = %+v, %s", closed, d.status(t, lee))
	}
	var moved struct {
		Items []struct {
			ID             string  `json:"id"`
			OtherContactID *string `json:"other_contact_id"`
		} `json:"items"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+printer+"/messages", nil, d.admin, nil), &moved)
	others := 0
	for _, item := range moved.Items {
		if item.OtherContactID != nil {
			others++
			if item.ID != leeMessage {
				t.Fatalf("another contact's message: %s", item.ID)
			}
		}
	}
	if others != 1 {
		t.Fatalf("messages of another contact = %d", others)
	}
	request(t, d.api, http.MethodPost, "/api/v1/message-moves/"+closed.Move.ID+"/undo", nil, d.admin, nil)
	if got := d.status(t, lee); got != "open" {
		t.Fatalf("source after undo = %s", got)
	}
	// Worked on: a note, then close_source is refused; and it moves every customer message or none.
	request(t, d.api, http.MethodPost, "/api/v1/conversations/"+lee+"/messages", map[string]any{"kind": "note", "body": "Called Lee"}, d.admin, nil)
	if response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+lee+"/messages/move", map[string]any{"message_ids": []string{leeMessage},
		"to": map[string]any{"conversation_id": printer}, "close_source": true}, d.admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "source_has_activity") || strings.Contains(response.Body.String(), "close_source") {
		// The web app shows the message as it comes, so it speaks of closing, not of a request field.
		t.Fatalf("close a worked-on conversation: %d %s", response.Code, response.Body.String())
	}
	d.move(t, d.admin, printer, map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": lee}, "close_source": true}, http.StatusBadRequest)
	// Mail of no chat scope has no intake queue.
	if response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+printer+"/messages/move", map[string]any{"message_ids": []string{first},
		"to": map[string]any{"queue": true}}, d.admin, nil); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "no_intake_queue") {
		t.Fatalf("email to the queue: %d %s", response.Code, response.Body.String())
	}
}

// No move reaches across workspaces: another workspace's conversation, message or move is not found.
func TestMessageMovesStayInTheirWorkspace(t *testing.T) {
	a := newMoveDesk(t, "Move A")
	b := newMoveDesk(t, "Move B")
	conversationA, messageA := a.mail(t, "From: kim@customer.example", "Subject: A", "Message-ID: <a-1@customer.example>", "A")
	otherA, _ := a.mail(t, "From: kim@customer.example", "Subject: A2", "Message-ID: <a-2@customer.example>", "A2")
	conversationB, messageB := b.mail(t, "From: kim@customer.example", "Subject: B", "Message-ID: <b-1@customer.example>", "B")
	moved := a.move(t, a.admin, conversationA, map[string]any{"message_ids": []string{messageA}, "to": map[string]any{"conversation_id": otherA}}, http.StatusOK)
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/conversations/" + otherA + "/messages/move", map[string]any{"message_ids": []string{messageA}, "to": map[string]any{"conversation_id": conversationB}}},
		{http.MethodGet, "/api/v1/conversations/" + otherA + "/message-moves", nil},
		{http.MethodPost, "/api/v1/message-moves/" + moved.Move.ID + "/undo", nil},
	} {
		if response := request(t, b.api, call.method, call.path, call.body, b.admin, nil); response.Code != http.StatusNotFound {
			t.Errorf("B %s %s: %d %s", call.method, call.path, response.Code, response.Body.String())
		}
	}
	// A's admin cannot take B's message nor send A's into B.
	a.move(t, a.admin, otherA, map[string]any{"message_ids": []string{messageA}, "to": map[string]any{"conversation_id": conversationB}}, http.StatusNotFound)
	a.move(t, a.admin, otherA, map[string]any{"message_ids": []string{messageB}, "to": map[string]any{"conversation_id": conversationA}}, http.StatusBadRequest)
}

// A conversation merged into another (closed as no new request) takes no moved message: it stays closed,
// and the refusal says where to move them instead.
func TestAMessageIsNotMovedIntoAConversationMergedAway(t *testing.T) {
	d := newMoveDesk(t, "Merged Desk")
	printer, first := d.mail(t, "From: kim@customer.example", "Subject: Printer", "Message-ID: <printer-m@customer.example>", "Printer")
	invoice, second := d.mail(t, "From: kim@customer.example", "Subject: Invoice", "Message-ID: <invoice-m@customer.example>", "Invoice")
	billing, _ := d.mail(t, "From: kim@customer.example", "Subject: Billing", "Message-ID: <billing-m@customer.example>", "Billing")
	d.move(t, d.admin, invoice, map[string]any{"message_ids": []string{second}, "to": map[string]any{"conversation_id": billing}, "close_source": true}, http.StatusOK)
	if got := d.status(t, invoice); got != "closed" {
		t.Fatalf("closed as no new request: %s", got)
	}
	response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+printer+"/messages/move",
		map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": invoice}}, d.admin, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "merged into another") {
		t.Fatalf("move into a merged conversation: %d %s", response.Code, response.Body.String())
	}
	if got := d.status(t, invoice); got != "closed" {
		t.Fatalf("the merged conversation is %s", got)
	}
}

// A move sent again under its Idempotency-Key answers the first move and moves nothing more (without the
// key a repeat finds the message gone and is refused); the key with other messages is refused with 422, and
// an undo sent again answers the same undone move.
func TestARepeatedKeyMovesMessagesOnce(t *testing.T) {
	d := newMoveDesk(t, "Move Keys")
	printer, first := d.mail(t, "From: kim@customer.example", "Subject: Printer", "Message-ID: <printer-k@customer.example>", "Printer")
	invoice, other := d.mail(t, "From: kim@customer.example", "Subject: Invoice", "Message-ID: <invoice-k@customer.example>", "Invoice")
	move := "/api/v1/conversations/" + printer + "/messages/move"
	body := map[string]any{"message_ids": []string{first}, "to": map[string]any{"conversation_id": invoice}}
	moved := postWithKey(t, d.api, d.admin, move, "move-1", body)
	if moved.Code != http.StatusOK {
		t.Fatalf("move: %d %s", moved.Code, moved.Body.String())
	}
	again := postWithKey(t, d.api, d.admin, move, "move-1", body)
	if again.Code != http.StatusOK || !sameAnswer(t, again, moved) {
		t.Fatalf("again: %d %s, want the first answer", again.Code, again.Body.String())
	}
	if fresh := postWithKey(t, d.api, d.admin, move, "move-2", body); fresh.Code != http.StatusBadRequest {
		t.Fatalf("a new key for the same move: %d %s, want it refused as before", fresh.Code, fresh.Body.String())
	}
	reused := postWithKey(t, d.api, d.admin, "/api/v1/conversations/"+invoice+"/messages/move", "move-1",
		map[string]any{"message_ids": []string{other}, "to": map[string]any{"conversation_id": printer}})
	if reused.Code != http.StatusUnprocessableEntity || !strings.Contains(reused.Body.String(), `"code":"idempotency_key_reused"`) {
		t.Fatalf("the key with other messages: %d %s", reused.Code, reused.Body.String())
	}
	var result moveResponse
	decode(t, moved, &result)
	moves := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+printer+"/message-moves", nil, d.admin, nil)
	if moves.Code != http.StatusOK || strings.Count(moves.Body.String(), `"from_conversation_id"`) != 1 {
		t.Fatalf("moves: %d %s, want one", moves.Code, moves.Body.String())
	}
	undo := "/api/v1/message-moves/" + result.Move.ID + "/undo"
	undone := postWithKey(t, d.api, d.admin, undo, "undo-1", nil)
	if undone.Code != http.StatusOK {
		t.Fatalf("undo: %d %s", undone.Code, undone.Body.String())
	}
	if again := postWithKey(t, d.api, d.admin, undo, "undo-1", nil); again.Code != http.StatusOK || !sameAnswer(t, again, undone) {
		t.Fatalf("undo again: %d %s", again.Code, again.Body.String())
	}
	if reused := postWithKey(t, d.api, d.admin, "/api/v1/message-moves/00000000-0000-4000-8000-000000000000/undo", "undo-1", nil); reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the undo key on another move: %d %s", reused.Code, reused.Body.String())
	}
}
