package freshdesk_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func TestImportNeverReusesContactsOrAgentsOfAnotherWorkspace(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	other, _, err := env.auth.CreateWorkspace(ctx, "Other Team", "bob@old.example")
	if err != nil {
		t.Fatal(err)
	}
	otherCarol, err := store.New(env.pool).CreateContact(ctx, other.ID, "Carol in another workspace", "carol@globex.example")
	if err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)

	var reused, foreignAuthors int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM import_items WHERE workspace_id=$1 AND target_id=$2`, env.workspaceID, otherCarol.ID).Scan(&reused); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM messages m JOIN agents a ON a.id=m.author_agent_id
		WHERE m.workspace_id=$1 AND a.workspace_id<>$1`, env.workspaceID).Scan(&foreignAuthors); err != nil {
		t.Fatal(err)
	}
	carolInA, err := store.New(env.pool).GetContact(ctx, env.workspaceID, env.target(t, "contact", "201"))
	if err != nil || reused != 0 || foreignAuthors != 0 || carolInA.Name != "Carol Customer" {
		t.Fatalf("cross-workspace reuse: ledger refs=%d foreign authors=%d contact=%+v err=%v", reused, foreignAuthors, carolInA.Contact, err)
	}
	var otherName string
	if err := env.pool.QueryRow(ctx, `SELECT name FROM contacts WHERE id=$1`, otherCarol.ID).Scan(&otherName); err != nil || otherName != "Carol in another workspace" {
		t.Fatalf("other workspace contact changed: %q %v", otherName, err)
	}
}

func TestRequesterFetchedByIDKeepsItsNameOnLaterRuns(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	env.fake.extraReplyOnTicket6 = true
	report := env.mustRun(t)
	if report.Status != "OK" {
		t.Fatalf("status=%s failures=%+v missing=%v", report.Status, report.Failures, report.Missing)
	}
	var author string
	if err := env.pool.QueryRow(context.Background(), `SELECT author_name FROM messages WHERE workspace_id=$1 AND id=$2`,
		env.workspaceID, env.target(t, "conversation", "7007")).Scan(&author); err != nil {
		t.Fatal(err)
	}
	if author != "Erin Late" {
		t.Fatalf("author of a new reply from a requester fetched by id = %q, want Erin Late", author)
	}
}

func TestSavedReplyImportStaysInItsWorkspace(t *testing.T) {
	env := newImportEnv(t).withCanned()
	ctx := context.Background()
	other, _, err := env.auth.CreateWorkspace(ctx, "Other Team", "bob@old.example")
	if err != nil {
		t.Fatal(err)
	}
	// The other workspace already uses a title the import would produce.
	if _, err := env.pool.Exec(ctx, `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,'General / Greeting','Theirs')`, other.ID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != "OK" || len(env.savedReplies(t)) != 5 {
		t.Fatalf("status=%s failures=%+v rows=%d", report.Status, report.Failures, len(env.savedReplies(t)))
	}
	for _, reply := range env.savedReplies(t) {
		if reply.Title == "General / Greeting (Freshdesk 9101)" {
			t.Fatal("a title used in another workspace was treated as taken")
		}
	}
	var theirs int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM saved_replies WHERE workspace_id=$1`, other.ID).Scan(&theirs); err != nil || theirs != 1 {
		t.Fatalf("other workspace holds %d saved replies (%v), want its own 1", theirs, err)
	}
}
