package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// heldCard writes a card of a chat's intake queue the way a held batch leaves it: its journal event
// held for intake, what the customer wrote, and the gathering in review. It returns the card's id.
func (d levelDesk) heldCard(t *testing.T, workspace, channel, from, text string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"id": "ev-" + from + "-" + text[:4], "from": from, "group": "g1", "text": text})
	var card string
	if err := d.pool.QueryRow(context.Background(), `WITH th AS (INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,opens_conversations)
			VALUES($1,$2,'group','group:g1',true) ON CONFLICT (workspace_id,channel_id,external_id) DO UPDATE SET updated_at=now() RETURNING id),
		g AS (INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,state,review_reason,rules_action,rules_rule,opened_at,closed_at)
			SELECT $1,$2,'group','thread:'||th.id||':levelchat_user:'||$3,th.id,'review','no_command','review','review_hold',now(),now() FROM th RETURNING id,thread_id),
		e AS (INSERT INTO channel_inbound_events(workspace_id,channel_id,external_event_id,state,raw,sender_key,hold_reason,gathering_id,received_at,attempts)
			SELECT $1,$2,'ev-'||$3||'-'||gen_random_uuid(),'held',$4,'loopback_user:'||$3,'intake',g.id,now(),1 FROM g RETURNING id,gathering_id)
		INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,sender_kind,sender_value,sender_name,body,inbound_event_id,gathering_id,disposition,occurred_at)
			SELECT $1,$2,g.thread_id,'in','customer','loopback_user',$3,'Minh '||$3,$5,e.id,g.id,'pending_review',now() FROM g, e RETURNING gathering_id::text`,
		workspace, channel, from, raw, text).Scan(&card); err != nil {
		t.Fatal(err)
	}
	return card
}

// No card, no decision and no count of one workspace's intake is reachable from another.
func TestTheIntakeQueueStaysInItsWorkspace(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	if response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings", nil, d.adminA, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), card) {
		t.Fatalf("A's queue: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings", nil, d.adminB, nil); response.Code != http.StatusOK || strings.Contains(response.Body.String(), card) {
		t.Fatalf("B's queue: %d %s", response.Code, response.Body.String())
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/intake/gatherings/" + card, nil},
		{http.MethodPost, "/api/v1/intake/gatherings/" + card + "/decide", map[string]any{"action": "ignore"}},
		{http.MethodPost, "/api/v1/intake/gatherings/" + card + "/requeue", nil},
	} {
		if response := request(t, d.api, call.method, call.path, call.body, d.adminB, nil); response.Code != http.StatusNotFound {
			t.Errorf("B %s %s: %d %s", call.method, call.path, response.Code, response.Body.String())
		}
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/intake/stats?channel_id="+d.chatA, nil, d.adminB, nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"gatherings":0`) {
		t.Fatalf("B's stats of A's channel: %d %s", response.Code, response.Body.String())
	}
	// A conversation of B cannot take A's card, nor A's conversation B's eyes.
	conversationB := queryString(t, d.pool, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Bo','bo@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,c.id,'B''s' FROM c RETURNING id::text`, d.workspaceB)
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "attach", "conversation_id": conversationB},
		d.adminA, nil); response.Code != http.StatusNotFound {
		t.Fatalf("attach to B's conversation: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversationB+"/intake", nil, d.adminA, nil); response.Code != http.StatusNotFound {
		t.Fatalf("A reads B's conversation intake: %d", response.Code)
	}
	if got := queryString(t, d.pool, `SELECT state FROM intake_gatherings WHERE id=$1`, card); got != "review" {
		t.Fatalf("card = %s", got)
	}
}

// A person opens a conversation from a card; the same decision again is the same, another is refused
// with how the card stands, and the conversation says why its message is there.
func TestAPersonDecidesAnIntakeCard(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	decide := func(body map[string]any) *httptestResponse {
		response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", body, d.adminA, nil)
		return &httptestResponse{Code: response.Code, Body: response.Body.String()}
	}
	first := decide(map[string]any{"action": "create", "subject": "Stock export"})
	var decided struct {
		State          string  `json:"state"`
		ConversationID *string `json:"conversation_id"`
		DecidedBy      *string `json:"decided_by"`
		Decisions      []struct {
			Decider string `json:"decider"`
			Action  string `json:"action"`
		} `json:"decisions"`
	}
	if first.Code != http.StatusOK || json.Unmarshal([]byte(first.Body), &decided) != nil || decided.State != "created" || decided.ConversationID == nil ||
		*decided.DecidedBy != "agent" || len(decided.Decisions) != 1 || decided.Decisions[0].Decider != "agent" {
		t.Fatalf("create: %+v", first)
	}
	if got := queryString(t, d.pool, `SELECT subject FROM conversations WHERE id=$1`, *decided.ConversationID); got != "Stock export" {
		t.Fatalf("subject = %s", got)
	}
	if again := decide(map[string]any{"action": "create", "subject": "Stock export"}); again.Code != http.StatusOK {
		t.Fatalf("again: %+v", again)
	}
	other := decide(map[string]any{"action": "ignore"})
	if other.Code != http.StatusConflict || !strings.Contains(other.Body, `"code":"gathering_decided"`) || !strings.Contains(other.Body, `"state":"created"`) ||
		!strings.Contains(other.Body, *decided.ConversationID) {
		t.Fatalf("another decision: %+v", other)
	}
	why := request(t, d.api, http.MethodGet, "/api/v1/conversations/"+*decided.ConversationID+"/intake", nil, d.adminA, nil)
	if why.Code != http.StatusOK || !strings.Contains(why.Body.String(), `"decision_basis":"person"`) || !strings.Contains(why.Body.String(), `"decision_rule":"intake_create"`) ||
		!strings.Contains(why.Body.String(), card) {
		t.Fatalf("why: %d %s", why.Code, why.Body.String())
	}
	if bad := decide(map[string]any{"action": "attach"}); bad.Code != http.StatusBadRequest {
		t.Fatalf("attach without a conversation: %+v", bad)
	}
}

// A token below the tier of a decision files it for a person, with what the customer said; opening a
// conversation needs the send tier, setting a card aside the draft tier.
func TestATokenBelowTheTierFilesAnIntakeDecision(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	draft := issueToken(t, d.api, d.adminA, "intake draft", "draft").Token
	read := issueToken(t, d.api, d.adminA, "intake read", "read").Token
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "ignore"}, read, nil); response.Code != http.StatusAccepted {
		t.Fatalf("read token ignores: %d %s", response.Code, response.Body.String())
	}
	filed := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "create"}, draft, nil)
	var request202 struct {
		RequestID string `json:"request_id"`
		Summary   string `json:"summary"`
	}
	if filed.Code != http.StatusAccepted || json.Unmarshal(filed.Body.Bytes(), &request202) != nil || !strings.Contains(request202.Summary, card) || strings.Contains(request202.Summary, "stock") {
		t.Fatalf("draft token creates: %d %s", filed.Code, filed.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT state FROM intake_gatherings WHERE id=$1`, card); got != "review" {
		t.Fatalf("a filed decision changed the card: %s", got)
	}
	if approved := approve(t, d.api, d.adminA, request202.RequestID, nil); approved.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT state||'/'||decided_by FROM intake_gatherings WHERE id=$1`, card); got != "created/agent" {
		t.Fatalf("after approval: %s", got)
	}
	second := d.heldCard(t, d.workspaceA, d.chatA, "u2", "meeting tomorrow")
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+second+"/decide", map[string]any{"action": "ignore"}, draft, nil); response.Code != http.StatusOK {
		t.Fatalf("draft token ignores: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings?state=ignored", nil, read, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), second) {
		t.Fatalf("ignored list: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+second+"/requeue", nil, draft, nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"review_reason":"requeued"`) {
		t.Fatalf("requeue: %d %s", response.Code, response.Body.String())
	}
}

// A card joins only a conversation where its reply goes to the same people: one of another place or
// customer or an email thread is refused, nothing is written and the card still waits.
func TestADecisionJoinsOnlyAConversationTheCardOffers(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	conversation := func(channel, status string) string {
		return queryString(t, d.pool, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Other',gen_random_uuid()||'@example.com') RETURNING id)
			INSERT INTO conversations(workspace_id,contact_id,subject,channel_id,status) SELECT $1,c.id,'Someone else''s',NULLIF($2,'')::uuid,$3 FROM c RETURNING id::text`,
			d.workspaceA, channel, status)
	}
	otherPlace := conversation(d.chatA, "open")
	closed := conversation(d.chatA, "closed")
	emailThread := conversation("", "resolved")
	messages := func() string {
		return queryString(t, d.pool, `SELECT count(*)::text FROM messages WHERE workspace_id=$1`, d.workspaceA)
	}
	before := messages()
	for name, target := range map[string]string{"another place": otherPlace, "closed in another place": closed, "an email thread": emailThread} {
		response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "attach", "conversation_id": target}, d.adminA, nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "may join") {
			t.Errorf("attach to %s: %d %s", name, response.Code, response.Body.String())
		}
	}
	if got := queryString(t, d.pool, `SELECT state FROM intake_gatherings WHERE id=$1`, card); got != "review" {
		t.Fatalf("card = %s", got)
	}
	if after := messages(); after != before {
		t.Fatalf("messages %s -> %s", before, after)
	}
	if got := queryString(t, d.pool, `SELECT status FROM conversations WHERE id=$1`, emailThread); got != "resolved" {
		t.Fatalf("the email thread is %s", got)
	}
}

// sameAnswer reports whether two responses carry the same JSON: a repeat under an Idempotency-Key answers
// the first answer as stored, whose keys may come back in another order.
func sameAnswer(t *testing.T, one, other *httptest.ResponseRecorder) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(one.Body.Bytes(), &a); err != nil {
		t.Fatalf("%s: %v", one.Body.String(), err)
	}
	if err := json.Unmarshal(other.Body.Bytes(), &b); err != nil {
		t.Fatalf("%s: %v", other.Body.String(), err)
	}
	return reflect.DeepEqual(a, b)
}

// A decision sent again under its Idempotency-Key (a double click, a retry after a lost answer) answers
// what the first one got and decides nothing more; the key with another decision is refused with 422.
func TestARepeatedKeyDecidesAnIntakeCardOnce(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	decide := "/api/v1/intake/gatherings/" + card + "/decide"
	body := map[string]any{"action": "create", "subject": "Stock export"}
	first := postWithKey(t, d.api, d.adminA, decide, "decide-1", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	again := postWithKey(t, d.api, d.adminA, decide, "decide-1", body)
	if again.Code != http.StatusOK || !sameAnswer(t, again, first) {
		t.Fatalf("again: %d %s, want the first answer", again.Code, again.Body.String())
	}
	other := postWithKey(t, d.api, d.adminA, decide, "decide-1", map[string]any{"action": "ignore"})
	if other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), `"code":"idempotency_key_reused"`) {
		t.Fatalf("the key with another decision: %d %s", other.Code, other.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM intake_decisions WHERE gathering_id=$1`, card); got != "1" {
		t.Fatalf("decisions = %s, want 1", got)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1 AND subject='Stock export'`, d.workspaceA); got != "1" {
		t.Fatalf("conversations opened = %s, want 1", got)
	}
	// Putting a card back takes a key too: the same key names one card.
	second := d.heldCard(t, d.workspaceA, d.chatA, "u2", "the invoice is wrong")
	requeue := func(id, key string) *httptest.ResponseRecorder {
		return postWithKey(t, d.api, d.adminA, "/api/v1/intake/gatherings/"+id+"/requeue", key, nil)
	}
	if response := postWithKey(t, d.api, d.adminA, "/api/v1/intake/gatherings/"+second+"/decide", "decide-2", map[string]any{"action": "ignore"}); response.Code != http.StatusOK {
		t.Fatalf("ignore: %d %s", response.Code, response.Body.String())
	}
	back := requeue(second, "requeue-1")
	if back.Code != http.StatusOK {
		t.Fatalf("requeue: %d %s", back.Code, back.Body.String())
	}
	if again := requeue(second, "requeue-1"); again.Code != http.StatusOK || !sameAnswer(t, again, back) {
		t.Fatalf("requeue again: %d %s", again.Code, again.Body.String())
	}
	if reused := requeue(card, "requeue-1"); reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the key on another card: %d %s", reused.Code, reused.Body.String())
	}
	if bad := postWithKey(t, d.api, d.adminA, decide, "two words", body); bad.Code != http.StatusBadRequest {
		t.Fatalf("a key with a space: %d %s", bad.Code, bad.Body.String())
	}
}

// groupConversation writes a conversation of the desk's group g1 (one of its items is linked to it), with
// a status and its last change hours ago; it returns its id.
func (d levelDesk) groupConversation(t *testing.T, workspace, channel, subject, status string, hours int) string {
	t.Helper()
	return queryString(t, d.pool, `WITH p AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Minh',$3||'@example.com') RETURNING id),
			c AS (INSERT INTO conversations(workspace_id,contact_id,subject,status,channel_id,updated_at)
				SELECT $1,p.id,$3,$4,$2,now()-make_interval(hours=>$5) FROM p RETURNING id),
			i AS (INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,body,disposition,occurred_at)
				SELECT $1,$2,t.id,'in','customer',$3,'conversation',now() FROM channel_threads t WHERE t.workspace_id=$1 AND t.channel_id=$2 AND t.external_id='group:g1'
				RETURNING id)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT $1,c.id,i.id,'opened' FROM c, i RETURNING conversation_id::text`,
		workspace, channel, subject, status, hours)
}

// A card shows what a person needs to decide it: the place, what the rules decided and on what basis, the
// conversations it may join without the reply going to someone else, and the channel's commands. Set
// aside, it keeps why; the lists narrow by who decided; the summary counts the queue of its workspace only.
func TestAnIntakeCardSaysWhatAPersonNeedsToDecideIt(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	if _, err := d.pool.Exec(context.Background(), `UPDATE channel_threads SET name='Kho HD' WHERE workspace_id=$1`, d.workspaceA); err != nil {
		t.Fatal(err)
	}
	open := d.groupConversation(t, d.workspaceA, d.chatA, "Printer", "open", 1)
	resolved := d.groupConversation(t, d.workspaceA, d.chatA, "Invoice", "resolved", 48)
	closed := d.groupConversation(t, d.workspaceA, d.chatA, "Closed", "closed", 1)
	quiet := d.groupConversation(t, d.workspaceA, d.chatA, "Quiet", "open", 100)
	old := d.groupConversation(t, d.workspaceA, d.chatA, "Old", "resolved", 24*8)
	var got struct {
		Rules struct {
			Basis string `json:"basis"`
		} `json:"rules"`
		Place *struct {
			Kind string  `json:"kind"`
			Name *string `json:"name"`
		} `json:"place"`
		IgnoreReason *string `json:"ignore_reason"`
		Candidates   []struct {
			ConversationID string `json:"conversation_id"`
			ContactName    string `json:"contact_name"`
			Resolved       bool   `json:"resolved"`
		} `json:"candidates"`
		Commands  []string `json:"commands"`
		Decisions []struct {
			IgnoreReason *string `json:"ignore_reason"`
		} `json:"decisions"`
	}
	read := func() {
		t.Helper()
		response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings/"+card, nil, d.adminA, nil)
		got.Candidates, got.Commands = nil, nil
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil {
			t.Fatalf("card: %d %s", response.Code, response.Body.String())
		}
	}
	read()
	if got.Rules.Basis != "default" || got.Place == nil || got.Place.Kind != "group" || got.Place.Name == nil || *got.Place.Name != "Kho HD" || got.IgnoreReason != nil {
		t.Fatalf("card = %+v", got)
	}
	if len(got.Candidates) != 2 || got.Candidates[0].ConversationID != open || got.Candidates[1].ConversationID != resolved || !got.Candidates[1].Resolved ||
		got.Candidates[0].ContactName != "Minh" {
		t.Fatalf("candidates = %+v (closed %s, quiet %s, old %s)", got.Candidates, closed, quiet, old)
	}
	if strings.Join(got.Commands, ",") != "bug" {
		t.Fatalf("commands = %v", got.Commands)
	}
	decide := func(body map[string]any) *httptestResponse {
		response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", body, d.adminA, nil)
		return &httptestResponse{Code: response.Code, Body: response.Body.String()}
	}
	for _, bad := range []map[string]any{{"action": "create", "ignore_reason": "chat"}, {"action": "ignore", "ignore_reason": "spam"}} {
		if response := decide(bad); response.Code != http.StatusBadRequest {
			t.Fatalf("%v: %+v", bad, response)
		}
	}
	if response := decide(map[string]any{"action": "ignore", "ignore_reason": "handled_in_chat"}); response.Code != http.StatusOK {
		t.Fatalf("ignore: %+v", response)
	}
	read()
	if got.IgnoreReason == nil || *got.IgnoreReason != "handled_in_chat" || len(got.Decisions) != 1 || got.Decisions[0].IgnoreReason == nil ||
		*got.Decisions[0].IgnoreReason != "handled_in_chat" || len(got.Candidates) != 0 {
		t.Fatalf("set aside = %+v", got)
	}
	conflict := decide(map[string]any{"action": "create"})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body, `"decided_by_agent_id":"`+agentIDOf(t, d.api, d.adminA)+`"`) {
		t.Fatalf("another decision: %+v", conflict)
	}
	for path, want := range map[string]bool{"state=ignored&decided_by=agent": true, "state=ignored&decided_by=ai": false, "state=decided&decided_by=agent": true} {
		response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings?"+path, nil, d.adminA, nil)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), card) != want {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings?decided_by=person", nil, d.adminA, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("decided_by=person: %d", response.Code)
	}
	d.heldCard(t, d.workspaceA, d.chatA, "u2", "the printer is down")
	var summary struct {
		Available bool `json:"available"`
		Waiting   int  `json:"waiting"`
		Deciding  int  `json:"deciding"`
		Scopes    []struct {
			ChannelID string `json:"channel_id"`
			Scope     string `json:"scope"`
		} `json:"scopes"`
	}
	response := request(t, d.api, http.MethodGet, "/api/v1/intake/summary", nil, d.adminA, nil)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &summary) != nil || !summary.Available || summary.Waiting != 1 || summary.Deciding != 0 ||
		len(summary.Scopes) != 1 || summary.Scopes[0].ChannelID != d.chatA || summary.Scopes[0].Scope != "group" {
		t.Fatalf("A's summary: %d %s", response.Code, response.Body.String())
	}
	response = request(t, d.api, http.MethodGet, "/api/v1/intake/summary", nil, d.adminB, nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), d.chatA) || !strings.Contains(response.Body.String(), `"waiting":0`) {
		t.Fatalf("B's summary: %d %s", response.Code, response.Body.String())
	}
}

// A token that may decide cards but not read what customers wrote gets the card back without it.
func TestDecidingACardWithoutMessagesReadReturnsNoCustomerText(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "SECRET stock export fails")
	token := issueCustomToken(t, d.api, d.adminA, "decider", "conversations:update").Token
	for _, call := range []struct {
		path string
		body any
	}{{"/decide", map[string]any{"action": "ignore"}}, {"/requeue", nil}} {
		response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+call.path, call.body, token, nil)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "SECRET") || strings.Contains(response.Body.String(), "Minh") ||
			!strings.Contains(response.Body.String(), `"state"`) {
			t.Fatalf("%s: %d %s", call.path, response.Code, response.Body.String())
		}
	}
}

// A channel with cards of the intake queue still to be decided is not removed, and the refusal says how
// many of its held messages are decided in the queue rather than opened or discarded one by one.
func TestRemovingAChannelSaysWhichHeldMessagesAreCardsOfTheQueue(t *testing.T) {
	d := newLevelDesk(t)
	d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	response := request(t, d.api, http.MethodDelete, "/api/v1/channels/"+d.chatA, nil, d.adminA, nil)
	var refused struct {
		Error struct {
			Code        string `json:"code"`
			HeldCount   int    `json:"held_count"`
			QueuedCount int    `json:"queued_count"`
			Message     string `json:"message"`
		} `json:"error"`
	}
	if response.Code != http.StatusConflict || json.Unmarshal(response.Body.Bytes(), &refused) != nil || refused.Error.Code != "channel_has_held" ||
		refused.Error.HeldCount != 1 || refused.Error.QueuedCount != 1 || !strings.Contains(refused.Error.Message, "queue") {
		t.Fatalf("remove: %d %s", response.Code, response.Body.String())
	}
}
