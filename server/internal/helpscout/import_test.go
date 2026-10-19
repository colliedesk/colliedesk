package helpscout_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/helpscout"
)

// The expected values below are counted by hand from testdata: 3 conversations in two pages (101,
// 102 | 103) plus spam 104; threads 2001-2009 of which 2004 is a line item and 2007 a draft;
// attachments 3001 and 3002; one hosted image in 2002; tags urgent, printer (101) and billing (102).
func TestImportCopiesTheInboxAndReconcilesExactCounts(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)

	if report.Status != helpscout.StatusOK {
		data, _ := json.MarshalIndent(report, "", " ")
		t.Fatalf("status = %s, want OK\n%s", report.Status, data)
	}
	if report.Source != "helpscout:mailbox:501" || report.Mailbox.ID != 501 || report.Mailbox.Name != "Support" {
		t.Fatalf("source=%q mailbox=%+v", report.Source, report.Mailbox)
	}
	want := map[string]helpscout.EntityCount{
		"conversations": {Source: 3, Imported: 3}, "contacts": {Source: 2, Imported: 2}, "messages": {Source: 7, Imported: 7},
		"attachments": {Source: 2, Imported: 2}, "inline images": {Source: 1, Imported: 1}, "tags": {Source: 3, Imported: 3},
	}
	got := map[string]helpscout.EntityCount{
		"conversations": report.Conversations.EntityCount, "contacts": report.Contacts, "messages": report.Messages.EntityCount,
		"attachments": report.Attachments.EntityCount, "inline images": report.InlineImages.EntityCount, "tags": report.Tags,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %+v\nwant     %+v", got, want)
	}
	if !reflect.DeepEqual(report.Conversations.Spam, []string{"104"}) || !reflect.DeepEqual(report.Messages.DraftThreads, []string{"2007"}) || report.Messages.LineItems != 1 {
		t.Fatalf("spam=%v drafts=%v line items=%d", report.Conversations.Spam, report.Messages.DraftThreads, report.Messages.LineItems)
	}
	if report.Attachments.ChecksumVerified != 2 || report.Attachments.ImportedBytes != 23 || report.InlineImages.ChecksumVerified != 1 {
		t.Fatalf("attachments verified=%d bytes=%d, inline verified=%d", report.Attachments.ChecksumVerified, report.Attachments.ImportedBytes, report.InlineImages.ChecksumVerified)
	}
	if report.Agents.Source != 3 || report.Agents.Matched != 1 || !reflect.DeepEqual(report.Agents.Unmatched, []string{"bob@acme.example", "carol@acme.example"}) {
		t.Fatalf("agents = %+v", report.Agents)
	}
	if !report.Verification.Conversations.Verified {
		t.Fatalf("conversations not verified: %+v", report.Verification.Conversations)
	}
	// Both pages of users, conversations and the threads of 101 were read.
	for _, key := range []string{"/v2/users?page=2", "/v2/conversations?page=2", "/v2/conversations/101/threads?page=2"} {
		if env.fake.count(key) == 0 {
			t.Errorf("%s was never requested", key)
		}
	}
	if got := env.tableCounts(t); got != "contacts=2 conversations=3 messages=7 attachments=3 tags=3 conversation_tags=3" {
		t.Fatalf("tables: %s", got)
	}
	if !strings.Contains(report.Markdown(), "# Help Scout import: OK") {
		t.Fatalf("markdown:\n%s", report.Markdown())
	}
}

func TestImportedConversationKeepsStatusAssigneeTimesAndCustomer(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	ctx := context.Background()
	type row struct {
		status, contactEmail, origin string
		assigned                     bool
		created                      time.Time
	}
	read := func(id string) row {
		var r row
		err := env.pool.QueryRow(ctx, `SELECT c.status,k.email,c.origin->>'external_id',c.assignee_id IS NOT NULL,c.created_at FROM conversations c
			JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`,
			env.workspaceID, env.target(t, "ticket", id)).Scan(&r.status, &r.contactEmail, &r.origin, &r.assigned, &r.created)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	want := map[string]row{
		"101": {"open", "dana@customer.example", "1001", true, time.Date(2026, 3, 2, 9, 15, 0, 0, time.UTC)},
		"102": {"closed", "erin@customer.example", "1002", false, time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)},
		"103": {"pending", "dana@customer.example", "1003", false, time.Date(2026, 3, 6, 14, 30, 0, 0, time.UTC)},
	}
	for id, expected := range want {
		got := read(id)
		got.created = got.created.UTC()
		if got != expected {
			t.Errorf("conversation %s = %+v, want %+v", id, got, expected)
		}
	}
}

func TestThreadsBecomeMessagesAndNotesStayInternal(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	want := map[string]struct{ kind, origin, author string }{
		"2001": {"incoming", "incoming", "Dana Lee"}, "2002": {"outgoing", "reply", "Alice Admin"},
		"2003": {"note", "private_note", "Bob Builder"}, "2005": {"incoming", "incoming", "Erin Moss"},
		"2006": {"outgoing", "reply", "Bob Builder"}, "2008": {"incoming", "incoming", "Dana Lee"},
		"2009": {"note", "other", "Carol Chen"},
	}
	for id, expected := range want {
		var kind, origin, author string
		var html *string
		err := env.pool.QueryRow(context.Background(), `SELECT kind,origin->>'kind',author_name,body_html FROM messages WHERE workspace_id=$1 AND id=$2`,
			env.workspaceID, env.target(t, "thread", id)).Scan(&kind, &origin, &author, &html)
		if err != nil {
			t.Fatal(err)
		}
		if kind != expected.kind || origin != expected.origin || author != expected.author {
			t.Errorf("thread %s = %s/%s/%s, want %+v", id, kind, origin, author, expected)
		}
		if id == "2002" && (html == nil || !strings.Contains(*html, `src="data:image/png;base64,`)) {
			t.Errorf("hosted image was not embedded: %v", html)
		}
	}
	var authorAgent bool
	if err := env.pool.QueryRow(context.Background(), `SELECT author_agent_id IS NOT NULL FROM messages WHERE workspace_id=$1 AND id=$2`,
		env.workspaceID, env.target(t, "thread", "2002")).Scan(&authorAgent); err != nil || !authorAgent {
		t.Fatalf("reply by alice is not linked to her agent: %v", err)
	}
}

func TestDryRunCountsAndWritesNothing(t *testing.T) {
	env := newImportEnv(t)
	report, err := env.run(t, func(o *helpscout.Options) { o.DryRun, o.Storage = true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != helpscout.StatusDryRun || report.Conversations.Source != 3 || report.Contacts.Source != 2 {
		t.Fatalf("dry run report: status=%s conversations=%d contacts=%d", report.Status, report.Conversations.Source, report.Contacts.Source)
	}
	if got := env.tableCounts(t); got != "contacts=0 conversations=0 messages=0 attachments=0 tags=0 conversation_tags=0" {
		t.Fatalf("dry run wrote: %s", got)
	}
}
