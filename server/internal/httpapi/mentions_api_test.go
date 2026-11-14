package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

type noteMentionView struct {
	AgentID string  `json:"agent_id"`
	Name    string  `json:"name"`
	ByEmail bool    `json:"by_email"`
	ReadAt  *string `json:"read_at"`
}

type mentionedNote struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Mentions []noteMentionView `json:"mentions"`
}

type mentionView struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	MessageID      string  `json:"message_id"`
	Subject        string  `json:"subject"`
	Excerpt        string  `json:"excerpt"`
	AuthorName     *string `json:"author_name"`
	AgentToken     *struct {
		Label     string `json:"label"`
		AgentName string `json:"agent_name"`
	} `json:"agent_token"`
	ReadAt *string `json:"read_at"`
}

type mentionList struct {
	Items      []mentionView `json:"items"`
	Unread     int           `json:"unread"`
	Email      bool          `json:"email"`
	NextBefore *string       `json:"next_before"`
}

type fieldError struct {
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	} `json:"error"`
}

func postNote(t *testing.T, f teamFixture, token, conversation, body string, mentions ...string) (int, mentionedNote, fieldError) {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "note", "body": body, "mentions": mentions}, token, nil)
	var note mentionedNote
	var failure fieldError
	if response.Code == http.StatusCreated {
		decode(t, response, &note)
	} else {
		decode(t, response, &failure)
	}
	return response.Code, note, failure
}

func myMentions(t *testing.T, f teamFixture, token, query string) mentionList {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/mentions"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list mentions status=%d body=%s", response.Code, response.Body.String())
	}
	var list mentionList
	decode(t, response, &list)
	return list
}

func markRead(t *testing.T, f teamFixture, token string, body map[string]any) (int, struct{ Changed, Unread int }) {
	t.Helper()
	response := request(t, f.api, http.MethodPut, "/api/v1/mentions/read", body, token, nil)
	var result struct{ Changed, Unread int }
	if response.Code == http.StatusOK {
		decode(t, response, &result)
	}
	return response.Code, result
}

func mentionEmails(t *testing.T, f teamFixture, agentID string) map[string]int {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT email,count(*) FROM note_mentions WHERE agent_id=$1 GROUP BY email`, agentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			t.Fatal(err)
		}
		counts[state] = count
	}
	return counts
}

func TestOnlyActiveTeammatesOfTheWorkspaceCanBeMentioned(t *testing.T) {
	f := newTeamFixture(t)
	conversation := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Refund still not on my card"})

	code, note, _ := postNote(t, f, f.admin, conversation, "@Ann @Bob can you check the provider?", f.annID, "BOB@team.example.com")
	if code != http.StatusCreated || len(note.Mentions) != 2 || note.Mentions[0].AgentID != f.annID || note.Mentions[1].AgentID != f.bobID {
		t.Fatalf("note with two teammates: code=%d mentions=%+v", code, note.Mentions)
	}
	if !note.Mentions[0].ByEmail || note.Mentions[0].ReadAt != nil || note.Mentions[0].Name != "ann@team.example.com" {
		t.Fatalf("a new mention is unread and emailed: %+v", note.Mentions[0])
	}

	otherWorkspaceAgent := workspaceAgentID(t, f, f.otherAdmin)
	if code, failure := f.patch(t, f.admin, f.bobID, map[string]any{"active": false}); code != http.StatusOK {
		t.Fatalf("deactivate bob: %d %+v", code, failure)
	}
	refused := map[string][]string{
		"a deactivated member":          {f.annID, f.bobID},
		"a customer's address":          {"customer@team.example.com"},
		"another workspace's agent":     {otherWorkspaceAgent},
		"another workspace's agent too": {"admin@other.example.com"},
	}
	for name, mentions := range refused {
		code, _, failure := postNote(t, f, f.admin, conversation, "Asking "+name, mentions...)
		place := "mentions.0"
		if name == "a deactivated member" {
			place = "mentions.1"
		}
		if code != http.StatusBadRequest || failure.Error.Fields[place] == "" {
			t.Errorf("%s: code=%d error=%+v, want 400 naming %s", name, code, failure.Error, place)
		}
	}
	// Nothing of a refused note was stored: Ann was told once, by the first note.
	if counts := mentionEmails(t, f, f.annID); counts["queued"] != 1 || len(counts) != 1 {
		t.Fatalf("ann's mention emails = %v, want one queued", counts)
	}

	reply := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Hello", "mentions": []string{f.annID}}, f.admin, nil)
	if reply.Code != http.StatusBadRequest {
		t.Fatalf("a reply with mentions: status=%d body=%s", reply.Code, reply.Body.String())
	}
	// The writer is never told of their own note.
	code, note, _ = postNote(t, f, f.admin, conversation, "Note to self", f.adminID)
	if code != http.StatusCreated || len(note.Mentions) != 0 {
		t.Fatalf("own mention: code=%d mentions=%+v", code, note.Mentions)
	}
}

func workspaceAgentID(t *testing.T, f teamFixture, token string) string {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/agents", nil, token, nil)
	var body struct {
		Items []teamMemberView `json:"items"`
	}
	decode(t, response, &body)
	return body.Items[0].ID
}

func TestMentionsListCountsUnreadAndMarksRead(t *testing.T) {
	f := newTeamFixture(t)
	refund := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Refund still not on my card"})
	invoice := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Invoice missing VAT"})
	if code, _, _ := postNote(t, f, f.admin, refund, "Bank confirmed RF-2291-08 lands by Thursday. @Ann can you check?", f.annID); code != http.StatusCreated {
		t.Fatalf("note: %d", code)
	}
	claude := customToken(t, f.api, f.admin, "Claude", "messages:read", "messages:note", "conversations:read")
	if code, _, _ := postNote(t, f, claude, invoice, "@Ann is the VAT number on the billing profile?", "ann@team.example.com"); code != http.StatusCreated {
		t.Fatalf("token note: %d", code)
	}

	list := myMentions(t, f, f.ann, "")
	if list.Unread != 2 || len(list.Items) != 2 || !list.Email {
		t.Fatalf("ann's mentions = %+v, want 2 unread and email on", list)
	}
	newest, oldest := list.Items[0], list.Items[1]
	if newest.Subject != "Invoice missing VAT" || newest.AgentToken == nil || newest.AgentToken.Label != "Claude" || newest.AuthorName == nil || *newest.AuthorName != "admin@team.example.com" {
		t.Fatalf("the token's mention reads <person> via <token>: %+v", newest)
	}
	if oldest.Subject != "Refund still not on my card" || oldest.Excerpt != "Bank confirmed RF-2291-08 lands by Thursday. @Ann can you check?" || oldest.AgentToken != nil || oldest.ReadAt != nil {
		t.Fatalf("the person's mention: %+v", oldest)
	}
	if mine := myMentions(t, f, f.bob, "?status=all"); len(mine.Items) != 0 || mine.Unread != 0 {
		t.Fatalf("bob was asked nothing: %+v", mine)
	}

	// Opening the conversation marks the mentions on it read; the writer's receipt shows when.
	if code, result := markRead(t, f, f.ann, map[string]any{"conversation_id": refund}); code != http.StatusOK || result.Changed != 1 || result.Unread != 1 {
		t.Fatalf("open the refund conversation: code=%d result=%+v", code, result)
	}
	if unread := myMentions(t, f, f.ann, ""); len(unread.Items) != 1 || unread.Items[0].ConversationID != invoice || unread.Unread != 1 {
		t.Fatalf("unread after opening one: %+v", unread)
	}
	timeline := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+refund+"/messages", nil, f.admin, nil)
	var messages struct {
		Items []mentionedNote `json:"items"`
	}
	decode(t, timeline, &messages)
	if len(messages.Items) != 1 || len(messages.Items[0].Mentions) != 1 || messages.Items[0].Mentions[0].ReadAt == nil {
		t.Fatalf("the writer sees when Ann saw it: %+v", messages.Items)
	}

	// Unread again, then Mark all read.
	if code, result := markRead(t, f, f.ann, map[string]any{"mention_ids": []string{oldest.ID}, "read": false}); code != http.StatusOK || result.Unread != 2 {
		t.Fatalf("mark unread: code=%d result=%+v", code, result)
	}
	if code, result := markRead(t, f, f.ann, map[string]any{"all": true}); code != http.StatusOK || result.Changed != 2 || result.Unread != 0 {
		t.Fatalf("mark all read: code=%d result=%+v", code, result)
	}
	if all := myMentions(t, f, f.ann, "?status=all"); len(all.Items) != 2 || all.Items[0].ReadAt == nil {
		t.Fatalf("all after mark all read: %+v", all)
	}

	// Another person's mention, or none of the three selectors, changes nothing.
	if code, _ := markRead(t, f, f.bob, map[string]any{"mention_ids": []string{oldest.ID}, "read": false}); code != http.StatusNotFound {
		t.Fatalf("bob marks ann's mention: %d, want 404", code)
	}
	if code, _ := markRead(t, f, f.ann, map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("no selector: %d, want 400", code)
	}
	if code, _ := markRead(t, f, f.ann, map[string]any{"all": true, "conversation_id": refund}); code != http.StatusBadRequest {
		t.Fatalf("two selectors: %d, want 400", code)
	}

	// Paging: one at a time, newest first.
	page := myMentions(t, f, f.ann, "?status=all&limit=1")
	if len(page.Items) != 1 || page.NextBefore == nil || page.Items[0].ID != newest.ID {
		t.Fatalf("first page: %+v", page)
	}
	next := myMentions(t, f, f.ann, "?status=all&limit=1&before="+*page.NextBefore)
	if len(next.Items) != 1 || next.Items[0].ID != oldest.ID || next.NextBefore != nil {
		t.Fatalf("second page: %+v", next)
	}
}

func TestAddingMentionsToANoteTellsOnlyTheNewPeople(t *testing.T) {
	f := newTeamFixture(t)
	conversation := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Export drops a column"})
	_, note, _ := postNote(t, f, f.admin, conversation, "@Ann have a look", f.annID)
	if code, _ := markRead(t, f, f.ann, map[string]any{"conversation_id": conversation}); code != http.StatusOK {
		t.Fatalf("ann reads: %d", code)
	}
	path := "/api/v1/conversations/" + conversation + "/messages/" + note.ID + "/mentions"
	response := request(t, f.api, http.MethodPut, path, map[string]any{"mentions": []string{f.annID, f.bobID}}, f.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("add bob: status=%d body=%s", response.Code, response.Body.String())
	}
	var edited mentionedNote
	decode(t, response, &edited)
	if len(edited.Mentions) != 2 || edited.Mentions[0].AgentID != f.annID || edited.Mentions[0].ReadAt == nil || edited.Mentions[1].AgentID != f.bobID {
		t.Fatalf("mentions after adding bob: %+v", edited.Mentions)
	}
	if ann, bob := mentionEmails(t, f, f.annID), mentionEmails(t, f, f.bobID); ann["queued"] != 1 || len(ann) != 1 || bob["queued"] != 1 || len(bob) != 1 {
		t.Fatalf("emails: ann=%v bob=%v, want one each", ann, bob)
	}
	if again := request(t, f.api, http.MethodPut, path, map[string]any{"mentions": []string{f.bobID}}, f.admin, nil); again.Code != http.StatusOK || mentionEmails(t, f, f.bobID)["queued"] != 1 {
		t.Fatalf("a repeat tells nobody again: status=%d", again.Code)
	}
	if bobs := myMentions(t, f, f.bob, ""); bobs.Unread != 1 {
		t.Fatalf("bob was told once: %+v", bobs)
	}

	// Only the writer adds to their note, and only to a note.
	if other := request(t, f.api, http.MethodPut, path, map[string]any{"mentions": []string{f.adminID}}, f.ann, nil); other.Code != http.StatusForbidden {
		t.Fatalf("ann adds to the admin's note: status=%d body=%s", other.Code, other.Body.String())
	}
	reply := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hi"}, f.admin, nil)
	var sent mentionedNote
	decode(t, reply, &sent)
	if onReply := request(t, f.api, http.MethodPut, "/api/v1/conversations/"+conversation+"/messages/"+sent.ID+"/mentions",
		map[string]any{"mentions": []string{f.bobID}}, f.admin, nil); onReply.Code != http.StatusBadRequest {
		t.Fatalf("mentions on a reply: status=%d", onReply.Code)
	}
}

func TestMentionsNeverCrossWorkspaces(t *testing.T) {
	f := newTeamFixture(t)
	conversation := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Secret"})
	_, note, _ := postNote(t, f, f.admin, conversation, "@Ann secret", f.annID)
	mention := myMentions(t, f, f.ann, "").Items[0]

	if theirs := myMentions(t, f, f.otherAdmin, "?status=all"); len(theirs.Items) != 0 || theirs.Unread != 0 {
		t.Fatalf("another workspace lists: %+v", theirs)
	}
	if code, _ := markRead(t, f, f.otherAdmin, map[string]any{"mention_ids": []string{mention.ID}}); code != http.StatusNotFound {
		t.Fatalf("another workspace marks a mention: %d", code)
	}
	if code, _ := markRead(t, f, f.otherAdmin, map[string]any{"conversation_id": conversation}); code != http.StatusNotFound {
		t.Fatalf("another workspace marks a conversation: %d", code)
	}
	other := workspaceAgentID(t, f, f.otherAdmin)
	if response := request(t, f.api, http.MethodPut, "/api/v1/conversations/"+conversation+"/messages/"+note.ID+"/mentions",
		map[string]any{"mentions": []string{other}}, f.otherAdmin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace adds to a note: status=%d", response.Code)
	}
	if unread := myMentions(t, f, f.ann, ""); unread.Unread != 1 || unread.Items[0].ReadAt != nil {
		t.Fatalf("ann's mention changed from another workspace: %+v", unread)
	}
}

func TestMentionEmailIsOnByDefaultAndEachPersonTurnsItOff(t *testing.T) {
	f := newTeamFixture(t)
	conversation := create(t, f.api, f.admin, "/api/v1/conversations", map[string]any{"contact_id": f.contactID, "subject": "Quiet"})
	response := request(t, f.api, http.MethodPatch, "/api/v1/agents/me/mention-email", map[string]any{"email": false}, f.ann, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("turn off: status=%d body=%s", response.Code, response.Body.String())
	}
	if mine := myMentions(t, f, f.ann, ""); mine.Email {
		t.Fatalf("ann's setting reads on after turning it off")
	}
	_, note, _ := postNote(t, f, f.admin, conversation, "@Ann @Bob quiet one", f.annID, f.bobID)
	if note.Mentions[0].ByEmail || !note.Mentions[1].ByEmail {
		t.Fatalf("by_email: %+v", note.Mentions)
	}
	if ann, bob := mentionEmails(t, f, f.annID), mentionEmails(t, f, f.bobID); ann["off"] != 1 || ann["queued"] != 0 || bob["queued"] != 1 {
		t.Fatalf("emails: ann=%v bob=%v", ann, bob)
	}
	if mine := myMentions(t, f, f.ann, ""); mine.Unread != 1 {
		t.Fatalf("off still reaches Mentions: %+v", mine)
	}
}
