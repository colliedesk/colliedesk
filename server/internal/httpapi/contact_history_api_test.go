package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type contactView struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	MergedIntoID *string `json:"merged_into_id"`
}

type contactMergeView struct {
	ID              string      `json:"id"`
	ContactID       string      `json:"contact_id"`
	MergedContact   contactView `json:"merged_contact"`
	ConversationIDs []string    `json:"conversation_ids"`
	UndoneAt        *string     `json:"undone_at"`
}

type contactDetailView struct {
	contactView
	ConversationCount int                `json:"conversation_count"`
	MergedInto        *contactView       `json:"merged_into"`
	Merges            []contactMergeView `json:"merges"`
}

func getContactDetail(t *testing.T, api http.Handler, token, id string) contactDetailView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/contacts/"+id, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET contact status=%d body=%s", response.Code, response.Body.String())
	}
	var detail contactDetailView
	decode(t, response, &detail)
	return detail
}

// contactHistory reads one page of a contact's conversations and returns their subjects in order.
func contactHistory(t *testing.T, api http.Handler, token, contact, extra string) []string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?contact="+url.QueryEscape(contact)+extra, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET contact history status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			Subject   string `json:"subject"`
			ContactID string `json:"contact_id"`
		} `json:"items"`
	}
	decode(t, response, &page)
	subjects := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		if item.ContactID != contact {
			t.Fatalf("history of %s listed a conversation of %s", contact, item.ContactID)
		}
		subjects = append(subjects, item.Subject)
	}
	return subjects
}

func searchContacts(t *testing.T, api http.Handler, token, search string) []string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/contacts?limit=20&search="+url.QueryEscape(search), nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("contact search status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []contactView `json:"items"`
	}
	decode(t, response, &list)
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names
}

func TestContactHistoryListsEveryConversationOfTheContactNewestFirstInPages(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "History Alpha", "admin@history-alpha.example.com")
	ada := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@history.example.com"})
	bob := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Bob", "email": "bob@history.example.com"})
	create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": bob, "subject": "Bob's question"})
	ids := make([]string, 0, 3)
	for index, subject := range []string{"Oldest", "Middle", "Newest"} {
		id := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": ada, "subject": subject})
		if _, err := pool.Exec(context.Background(), `UPDATE conversations SET updated_at=$2 WHERE id=$1`, id, time.Date(2026, 1, 1+index, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// Any status counts, and an imported conversation is history like any other.
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+ids[0], map[string]any{"status": "closed"}, token, nil); response.Code != http.StatusOK {
		t.Fatalf("close status=%d", response.Code)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET origin='{"system":"freshdesk","external_id":"7"}', updated_at='2026-01-01' WHERE id=$1`, ids[0]); err != nil {
		t.Fatal(err)
	}

	if got := contactHistory(t, api, token, ada, ""); strings.Join(got, ",") != "Newest,Middle,Oldest" {
		t.Fatalf("history=%v, want every conversation of Ada newest first", got)
	}
	if got := contactHistory(t, api, token, ada, "&page_size=2&page=2"); strings.Join(got, ",") != "Oldest" {
		t.Fatalf("second page=%v, want [Oldest]", got)
	}
	if detail := getContactDetail(t, api, token, ada); detail.ConversationCount != 3 || detail.MergedInto != nil || len(detail.Merges) != 0 {
		t.Fatalf("contact detail=%+v, want 3 conversations and no merges", detail)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversations?contact=not-a-uuid", nil, token, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("a contact that is not a UUID answered %d, want 400", response.Code)
	}
	// An assignee the list does not know is refused, never ignored: ignoring it would list everything.
	if response := request(t, api, http.MethodGet, "/api/v1/conversations?assignee=mee", nil, token, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown assignee answered %d %s, want 400", response.Code, response.Body.String())
	}
	for _, assignee := range []string{"me", "unassigned", "anyone"} {
		if response := request(t, api, http.MethodGet, "/api/v1/conversations?assignee="+assignee, nil, token, nil); response.Code != http.StatusOK {
			t.Fatalf("assignee=%s answered %d, want 200", assignee, response.Code)
		}
	}
}

func TestContactPageAndContactSearchStayInTheirWorkspace(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Isolation Alpha", "admin@isolation-alpha.example.com")
	tokenB := createSession(t, api, service, "Isolation Bravo", "admin@isolation-bravo.example.com")
	ada := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Ada Shared", "email": "ada@shared.example.com"})
	create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": ada, "subject": "Alpha only"})
	zed := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Zed Shared", "email": "zed@shared.example.com"})
	create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": zed, "subject": "Bravo only"})

	if got := searchContacts(t, api, tokenA, "shared"); strings.Join(got, ",") != "Ada Shared" {
		t.Fatalf("search in Alpha=%v, want only Alpha's contact", got)
	}
	if got := contactHistory(t, api, tokenA, zed, ""); len(got) != 0 {
		t.Fatalf("Alpha read Bravo's contact history: %v", got)
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/contacts/" + zed, nil},
		{http.MethodPatch, "/api/v1/contacts/" + zed, map[string]any{"name": "Taken over"}},
		{http.MethodPost, "/api/v1/contacts/" + zed + "/merges", map[string]any{"contact_id": ada}},
		{http.MethodPost, "/api/v1/contacts/" + ada + "/merges", map[string]any{"contact_id": zed}},
	} {
		if response := request(t, api, call.method, call.path, call.body, tokenA, nil); response.Code != http.StatusNotFound {
			t.Errorf("%s %s with another workspace's contact answered %d, want 404: %s", call.method, call.path, response.Code, response.Body.String())
		}
	}
	if detail := getContactDetail(t, api, tokenB, zed); detail.Name != "Zed Shared" || detail.MergedInto != nil || detail.ConversationCount != 1 {
		t.Fatalf("Bravo's contact changed: %+v", detail)
	}
	if detail := getContactDetail(t, api, tokenA, ada); len(detail.Merges) != 0 || detail.ConversationCount != 1 {
		t.Fatalf("Alpha's contact changed: %+v", detail)
	}
}

// After a merge, mail from the merged address opens a conversation on the kept contact, whose address
// never wrote. On the trial that reply is a first email to a new address: refused, saying why, until the
// sending domain is verified; then it goes to the kept contact's address.
func TestTrialReplyToNewMailFromAMergedAddressWaitsForAVerifiedDomain(t *testing.T) {
	f := newLimitsFixture(t, true)
	first := f.receive(t, false, 1, "First question")
	merged := queryString(t, f.pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, *first.ConversationID)
	kept := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Kept", "email": "kept@customer.example"})
	if response := request(t, f.api, http.MethodPost, "/api/v1/contacts/"+kept+"/merges", map[string]any{"contact_id": merged}, f.adminA, nil); response.Code != http.StatusCreated {
		t.Fatalf("merge status=%d body=%s", response.Code, response.Body.String())
	}
	later := inboundOK(t, f.api, platformSecret, mail([]string{
		"X-Original-To: support@limits-alpha." + hostedDomain,
		"From: customer1@customer.example", "To: support@limits-alpha." + hostedDomain,
		"Subject: Second question", "Message-ID: <limits-merged-later@customer.example>",
	}, "Another question from the merged address."))
	if later.Status != "created" || later.ConversationID == nil {
		t.Fatalf("new mail from the merged address = %+v, want a new conversation", later)
	}
	if owner := queryString(t, f.pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, *later.ConversationID); owner != kept {
		t.Fatalf("new mail went to contact %s, want the kept contact %s", owner, kept)
	}
	got := f.reply(t, f.adminA, *later.ConversationID)
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "trial reply to a kept contact whose address never wrote")
	// The customer did write first, from the merged address: the refusal says so, and where the reply would go.
	for _, want := range []string{"wrote to you first", "another address", `"reason":"contact_address_differs"`, `"reply_to":"kept@customer.example"`} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("the refusal lacks %s: %s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "start new conversations") {
		t.Fatalf("the refusal speaks of new conversations to a customer who wrote: %s", got.Body)
	}
	if count := f.outboxCount(t, f.workspaceA); count != "0" {
		t.Fatalf("outbox=%s, want nothing queued", count)
	}
	f.verifyDomain(t, f.workspaceA)
	f.wantReplyQueued(t, *later.ConversationID, "reply once the sending domain is verified")
	if to := queryString(t, f.pool, `SELECT k.email FROM email_outbox o JOIN messages m ON m.id=o.message_id
		JOIN conversations c ON c.id=m.conversation_id JOIN contacts k ON k.id=c.contact_id WHERE o.workspace_id=$1`, f.workspaceA); to != "kept@customer.example" {
		t.Fatalf("the reply is addressed to %s, want the kept contact's address", to)
	}
}

func TestAnotherWorkspaceCannotUndoAMerge(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Undo Alpha", "admin@undo-alpha.example.com")
	tokenB := createSession(t, api, service, "Undo Bravo", "admin@undo-bravo.example.com")
	ownA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@undo.example.com"})
	keptB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Kept", "email": "kept@undo.example.com"})
	duplicateB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Duplicate", "email": "dup@undo.example.com"})
	create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": duplicateB, "subject": "Bravo history"})
	mergeB := create(t, api, tokenB, "/api/v1/contacts/"+keptB+"/merges", map[string]any{"contact_id": duplicateB})

	for _, kept := range []string{keptB, ownA} {
		if response := request(t, api, http.MethodDelete, "/api/v1/contacts/"+kept+"/merges/"+mergeB, nil, tokenA, nil); response.Code != http.StatusNotFound {
			t.Errorf("undoing Bravo's merge from Alpha (contact %s) answered %d, want 404: %s", kept, response.Code, response.Body.String())
		}
	}
	if detail := getContactDetail(t, api, tokenB, keptB); len(detail.Merges) != 1 || detail.Merges[0].ID != mergeB || detail.ConversationCount != 1 {
		t.Fatalf("Bravo's merge changed: %+v", detail)
	}
	if got := contactHistory(t, api, tokenB, duplicateB, ""); len(got) != 0 {
		t.Fatalf("Bravo's moved conversation went back: %v", got)
	}
}

func TestAddingAContactThatExistsSaysWhichOneToUse(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Exists Desk", "admin@exists.example.com")
	kept := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Dana Work", "email": "dana@work.example"})
	duplicate := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Dana Home", "email": "dana@home.example"})
	create(t, api, token, "/api/v1/contacts/"+kept+"/merges", map[string]any{"contact_id": duplicate})

	again := request(t, api, http.MethodPost, "/api/v1/contacts", map[string]any{"name": "Dana", "email": "Dana@Work.example"}, token, nil)
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "contact_exists") || !strings.Contains(again.Body.String(), "Dana Work") {
		t.Fatalf("an existing address answered %d %s, want 409 contact_exists naming the customer", again.Code, again.Body.String())
	}
	merged := request(t, api, http.MethodPost, "/api/v1/contacts", map[string]any{"name": "Dana", "email": "dana@home.example"}, token, nil)
	if merged.Code != http.StatusConflict || !strings.Contains(merged.Body.String(), "dana@work.example") {
		t.Fatalf("a merged address answered %d %s, want 409 naming the contact it was merged into", merged.Code, merged.Body.String())
	}
}

func TestContactCanBeRenamedAndRenamedBack(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Rename Desk", "admin@rename.example.com")
	ada := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "ada@rename.example.com", "email": "ada@rename.example.com"})
	conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": ada, "subject": "Hello"})

	rename := func(name string, want int) {
		t.Helper()
		response := request(t, api, http.MethodPatch, "/api/v1/contacts/"+ada, map[string]any{"name": name}, token, nil)
		if response.Code != want {
			t.Fatalf("rename to %q status=%d body=%s, want %d", name, response.Code, response.Body.String(), want)
		}
	}
	rename("Ada Lovelace", http.StatusOK)
	if detail := getContactDetail(t, api, token, ada); detail.Name != "Ada Lovelace" || detail.Email != "ada@rename.example.com" {
		t.Fatalf("after rename: %+v", detail)
	}
	var got struct {
		ContactName string `json:"contact_name"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, token, nil), &got)
	if got.ContactName != "Ada Lovelace" {
		t.Fatalf("conversation shows %q, want the new name", got.ContactName)
	}
	rename("   ", http.StatusBadRequest)
	rename(strings.Repeat("a", 201), http.StatusBadRequest)
	rename("ada@rename.example.com", http.StatusOK)
	if response := request(t, api, http.MethodPatch, "/api/v1/contacts/00000000-0000-4000-8000-000000000000", map[string]any{"name": "Nobody"}, token, nil); response.Code != http.StatusNotFound {
		t.Fatalf("rename of an unknown contact answered %d, want 404", response.Code)
	}
}

func TestMergingContactsMovesConversationsRoutesMailAndCanBeUndone(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Merge Desk", "admin@merge.example.com")
	secret := connectEmail(t, api, admin, "support@merge.example.com")
	kept := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Dana Work", "email": "dana@work.example"})
	create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": kept, "subject": "From work"})
	first := inboundOK(t, api, secret, mail([]string{
		"From: Dana <dana@home.example>", "To: support@merge.example.com", "Subject: From home", "Message-ID: <home-1@home.example>",
	}, "Hi from home."))
	duplicate := queryString(t, pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, *first.ConversationID)

	merge := func(into, from string, want int) *contactMergeView {
		t.Helper()
		response := request(t, api, http.MethodPost, "/api/v1/contacts/"+into+"/merges", map[string]any{"contact_id": from}, admin, nil)
		if response.Code != want {
			t.Fatalf("merge %s into %s status=%d body=%s, want %d", from, into, response.Code, response.Body.String(), want)
		}
		if want != http.StatusCreated {
			return nil
		}
		var view contactMergeView
		decode(t, response, &view)
		return &view
	}
	merge(kept, kept, http.StatusConflict)
	done := merge(kept, duplicate, http.StatusCreated)
	if done.ContactID != kept || done.MergedContact.ID != duplicate || strings.Join(done.ConversationIDs, ",") != *first.ConversationID {
		t.Fatalf("merge=%+v, want the home conversation moved to the kept contact", done)
	}
	if got := contactHistory(t, api, admin, kept, ""); len(got) != 2 {
		t.Fatalf("kept contact history=%v, want both conversations", got)
	}
	if got := searchContacts(t, api, admin, "dana"); strings.Join(got, ",") != "Dana Work" {
		t.Fatalf("search=%v, want the merged duplicate left out", got)
	}
	// A search for the duplicate's own address finds the contact it was merged into.
	if got := searchContacts(t, api, admin, "home.example"); strings.Join(got, ",") != "Dana Work" {
		t.Fatalf("search for the merged address=%v, want the kept contact", got)
	}
	gone := getContactDetail(t, api, admin, duplicate)
	if gone.MergedInto == nil || gone.MergedInto.ID != kept || gone.Email != "dana@home.example" || gone.ConversationCount != 0 {
		t.Fatalf("merged contact=%+v, want it kept with its email and pointing at the kept contact", gone)
	}
	if detail := getContactDetail(t, api, admin, kept); len(detail.Merges) != 1 || detail.Merges[0].ID != done.ID {
		t.Fatalf("kept contact merges=%+v, want the merge listed", detail.Merges)
	}
	// Once merged, neither side can be merged again, and the duplicate cannot be merged into.
	other := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Other", "email": "other@merge.example"})
	merge(other, duplicate, http.StatusConflict)
	merge(duplicate, other, http.StatusConflict)
	merge(other, kept, http.StatusConflict)
	if response := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": duplicate, "subject": "To the old row"}, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("a conversation with a merged contact answered %d, want 409", response.Code)
	}

	// New mail from the duplicate's address joins the kept contact instead of recreating the duplicate.
	later := inboundOK(t, api, secret, mail([]string{
		"From: dana@home.example", "To: support@merge.example.com", "Subject: Again from home", "Message-ID: <home-2@home.example>",
	}, "Another question."))
	if owner := queryString(t, pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, *later.ConversationID); owner != kept {
		t.Fatalf("mail from the merged address went to %s, want the kept contact %s", owner, kept)
	}

	undo := request(t, api, http.MethodDelete, "/api/v1/contacts/"+kept+"/merges/"+done.ID, nil, admin, nil)
	if undo.Code != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", undo.Code, undo.Body.String())
	}
	var undone contactMergeView
	decode(t, undo, &undone)
	if undone.UndoneAt == nil {
		t.Fatalf("undone merge=%+v, want undone_at", undone)
	}
	if got := contactHistory(t, api, admin, duplicate, ""); strings.Join(got, ",") != "From home" {
		t.Fatalf("after undo the duplicate has %v, want exactly the conversation the merge moved", got)
	}
	if got := contactHistory(t, api, admin, kept, ""); len(got) != 2 {
		t.Fatalf("after undo the kept contact has %v, want its own and the one that arrived after the merge", got)
	}
	if detail := getContactDetail(t, api, admin, duplicate); detail.MergedInto != nil {
		t.Fatalf("after undo the duplicate is still merged: %+v", detail)
	}
	if detail := getContactDetail(t, api, admin, kept); len(detail.Merges) != 0 {
		t.Fatalf("after undo the merge is still listed: %+v", detail.Merges)
	}
	if again := request(t, api, http.MethodDelete, "/api/v1/contacts/"+kept+"/merges/"+done.ID, nil, admin, nil); again.Code != http.StatusConflict {
		t.Fatalf("undoing twice answered %d, want 409", again.Code)
	}
	if missing := request(t, api, http.MethodDelete, "/api/v1/contacts/"+kept+"/merges/00000000-0000-4000-8000-000000000000", nil, admin, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("undoing an unknown merge answered %d, want 404", missing.Code)
	}
	if got := searchContacts(t, api, admin, "dana"); len(got) != 2 {
		t.Fatalf("after undo search=%v, want both contacts", got)
	}
}

func TestTrialCannotReachAnotherAddressByMergingContacts(t *testing.T) {
	f := newLimitsFixture(t, true)
	wrote := f.receive(t, false, 1, "Real question")
	cold := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Cold", "email": "cold@elsewhere.example"})
	writer := queryString(t, f.pool, `SELECT contact_id::text FROM conversations WHERE id=$1`, *wrote.ConversationID)
	if response := request(t, f.api, http.MethodPost, "/api/v1/contacts/"+cold+"/merges", map[string]any{"contact_id": writer}, f.adminA, nil); response.Code != http.StatusCreated {
		t.Fatalf("merge status=%d body=%s", response.Code, response.Body.String())
	}
	// The conversation now belongs to an address that never wrote: on the trial that is a cold email.
	wantStatus(t, f.reply(t, f.adminA, *wrote.ConversationID), http.StatusForbidden, restrictedCode, "reply after merging into a cold contact")
	if count := f.outboxCount(t, f.workspaceA); count != "0" {
		t.Fatalf("outbox=%s, want nothing queued", count)
	}
	merge := queryString(t, f.pool, `SELECT id::text FROM contact_merges WHERE workspace_id=$1`, f.workspaceA)
	if response := request(t, f.api, http.MethodDelete, fmt.Sprintf("/api/v1/contacts/%s/merges/%s", cold, merge), nil, f.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", response.Code, response.Body.String())
	}
	f.wantReplyQueued(t, *wrote.ConversationID, "reply after undoing the merge")
}
