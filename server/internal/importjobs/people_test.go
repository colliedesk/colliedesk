package importjobs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

const peopleSource = "freshdesk:account:777"

// previewWithAgents runs a preview through the worker with Alice (the workspace admin's email) and Bob.
func (e *env) previewWithAgents(t *testing.T) importjobs.Job {
	t.Helper()
	var adminEmail string
	if err := e.pool.QueryRow(context.Background(), `SELECT email FROM agents WHERE id=$1`, e.a.agentID).Scan(&adminEmail); err != nil {
		t.Fatal(err)
	}
	job := e.preview(t, e.a)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, nil, func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) {
		return &freshdesk.PreviewResult{Domain: "acme.freshdesk.com", AccountID: 777, Source: peopleSource, Agents: []freshdesk.PreviewAgent{
			{ID: 101, Name: "Alice", Email: adminEmail, Active: true}, {ID: 102, Name: "Bob", Email: "bob@old.example", Active: false},
		}}, nil
	}, nil)
	tick(t, w)
	return job
}

// seedImported writes what an import of the account leaves for agent 102: two replies and two tickets
// assigned to them, one of which a person reassigned since. It returns the reassigned ticket's agent.
func (e *env) seedImported(t *testing.T) (other string) {
	t.Helper()
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'carol@acme.example','agent')`, e.a.id)
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE email='carol@acme.example'`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Customer','customer@example.com') RETURNING id),
		conv AS (INSERT INTO conversations(workspace_id,contact_id,subject,assignee_id,origin,channel)
			SELECT $1,c.id,'Ticket '||n,CASE WHEN n=2 THEN $2::uuid END,jsonb_build_object('system','freshdesk','external_id',n::text,'responder_external_id','102'),'imported'
			FROM c, generate_series(1,2) n RETURNING id, origin->>'external_id' AS ext),
		ledger AS (INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status) SELECT $1,$3,'ticket',ext,id,'done' FROM conv),
		msg AS (INSERT INTO messages(workspace_id,conversation_id,kind,body,author_name,origin)
			SELECT $1,conv.id,'outgoing','Hello','Bob',jsonb_build_object('system','freshdesk','external_id','70'||conv.ext,'kind','reply','author_external_id','102') FROM conv RETURNING id, origin->>'external_id' AS ext)
		INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status) SELECT $1,$3,'conversation',ext,id,'done' FROM msg`, e.a.id, other, peopleSource)
	return other
}

func (e *env) authors(t *testing.T) (linked, assigned int, assignees map[string]int) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND author_agent_id=$2`, e.a.id, e.a.agentID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND assignee_id=$2`, e.a.id, e.a.agentID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	return linked, assigned, nil
}

func TestThePreviewListsTheFreshdeskAgentsAndLinksTheOnesWithTheSameEmail(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewWithAgents(t)
	people, err := e.service.People(context.Background(), e.a.id, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 || people[0].ExternalID != "101" || people[0].LinkedAgentID == nil || *people[0].LinkedAgentID != e.a.agentID || !people[0].LinkedByEmail ||
		people[1].ExternalID != "102" || people[1].LinkedAgentID != nil {
		t.Fatalf("people = %+v", people)
	}
}

func TestLinkingAFreshdeskAgentMovesTheirImportedRepliesAndTicketsAndUnlinkingTakesThemBack(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewWithAgents(t)
	other := e.seedImported(t)
	w := e.worker(t, e.keys)
	person, err := e.service.Link(context.Background(), e.a.id, job.ID, "102", e.a.agentID)
	if err != nil || !person.Pending || person.LinkedAgentID == nil {
		t.Fatalf("link = %+v, %v", person, err)
	}
	tick(t, w)
	linked, assigned, _ := e.authors(t)
	if linked != 2 || assigned != 1 {
		t.Fatalf("after the link: %d replies, %d tickets of the agent; want 2 and 1 (a person reassigned the other)", linked, assigned)
	}
	var stillOther int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE assignee_id=$1`, other).Scan(&stillOther); err != nil || stillOther != 1 {
		t.Fatalf("the reassigned ticket moved: %d (%v)", stillOther, err)
	}
	people, _ := e.service.People(context.Background(), e.a.id, job.ID)
	if people[1].Pending {
		t.Fatalf("the link is still pending after the worker ran: %+v", people[1])
	}

	if _, err := e.service.Unlink(context.Background(), e.a.id, job.ID, "102"); err != nil {
		t.Fatal(err)
	}
	tick(t, w)
	if linked, assigned, _ := e.authors(t); linked != 0 || assigned != 0 {
		t.Fatalf("after the unlink: %d replies, %d tickets; want 0 and 0", linked, assigned)
	}
	var names int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE workspace_id=$1 AND author_name='Bob'`, e.a.id).Scan(&names); err != nil || names != 2 {
		t.Fatalf("the replies lost the Freshdesk name: %d (%v)", names, err)
	}
}

func TestAnAgentWhoJoinsIsLinkedToTheFreshdeskAgentWithTheirEmail(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewWithAgents(t)
	e.seedImported(t)
	e.exec(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'BOB@old.example','agent')`, e.a.id)
	var bob string
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE email='BOB@old.example'`).Scan(&bob); err != nil {
		t.Fatal(err)
	}
	n, err := e.service.LinkByEmail(context.Background(), e.a.id, bob, "BOB@old.example")
	if err != nil || n != 1 {
		t.Fatalf("linked %d, %v", n, err)
	}
	tick(t, e.worker(t, e.keys))
	var replies int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE author_agent_id=$1`, bob).Scan(&replies); err != nil || replies != 2 {
		t.Fatalf("Bob's replies = %d (%v)", replies, err)
	}
	people, _ := e.service.People(context.Background(), e.a.id, job.ID)
	if !people[1].LinkedByEmail {
		t.Fatalf("people = %+v", people)
	}
}

func TestPeopleOfAnotherWorkspaceCannotBeSeenOrLinked(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewWithAgents(t)
	ctx := context.Background()
	if _, err := e.service.People(ctx, e.b.id, job.ID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("people: %v", err)
	}
	if _, err := e.service.Link(ctx, e.b.id, job.ID, "102", e.b.agentID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("link: %v", err)
	}
	if _, err := e.service.Unlink(ctx, e.b.id, job.ID, "101"); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("unlink: %v", err)
	}
	// An agent of another workspace is not an agent to link to.
	if _, err := e.service.Link(ctx, e.a.id, job.ID, "102", e.b.agentID); !errors.Is(err, importjobs.ErrAgent) {
		t.Errorf("link to another workspace's agent: %v", err)
	}
	if n, err := e.service.LinkByEmail(ctx, e.b.id, e.b.agentID, "bob@old.example"); err != nil || n != 0 {
		t.Errorf("link by email in another workspace: %d, %v", n, err)
	}
	if _, err := e.service.Link(ctx, e.a.id, job.ID, "999", e.a.agentID); !errors.Is(err, importjobs.ErrNotFound) {
		t.Errorf("unknown Freshdesk agent: %v", err)
	}
}
