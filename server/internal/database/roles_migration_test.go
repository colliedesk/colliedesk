package database

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const rolesMigration = "20261102120000-roles.up.sql"

// migrateBeforeRoles applies every migration before roles and seeds two workspaces as the previous
// release would have them: an admin, an agent and a deactivated agent; an admin's session limited to
// the agent role; a send token; an unused admin invite and an agent one; a pending reply draft on an
// open conversation and one on a closed conversation, and a pending action.
func migrateBeforeRoles(t *testing.T) (*pgxpool.Pool, map[string]string) {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < rolesMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	ids := map[string]string{}
	for _, slug := range []string{"one", "two"} {
		var ws string
		if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(&ws); err != nil {
			t.Fatal(err)
		}
		ids[slug] = ws
		for _, person := range []struct{ key, role, deactivated string }{{"admin", "admin", ""}, {"agent", "agent", ""}, {"gone", "agent", "now()"}} {
			var id string
			deactivated := "NULL"
			if person.deactivated != "" {
				deactivated = person.deactivated
			}
			if err := pool.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role,deactivated_at) VALUES($1,$2,$3,`+deactivated+`) RETURNING id::text`,
				ws, person.key+"@"+slug+".example", person.role).Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids[slug+"/"+person.key] = id
		}
		var limited, token string
		if err := pool.QueryRow(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at)
			VALUES($1,$2,convert_to($3,'UTF8'),'agent','Limited',now()+interval '1 day') RETURNING id::text`, ws, ids[slug+"/admin"], "limited-"+slug).Scan(&limited); err != nil {
			t.Fatal(err)
		}
		ids[slug+"/limited"] = limited
		if err := pool.QueryRow(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			VALUES($1,$2,convert_to($3,'UTF8'),'agent','send','Claude',now()+interval '1 day') RETURNING id::text`, ws, ids[slug+"/agent"], "token-"+slug).Scan(&token); err != nil {
			t.Fatal(err)
		}
		ids[slug+"/token"] = token
		for _, ceiling := range []string{"admin", "agent"} {
			var link string
			if err := pool.QueryRow(ctx, `INSERT INTO pairing_links(workspace_id,email,kind,role_ceiling,token_hash,expires_at)
				VALUES($1,$2,'invite',$3,convert_to($4,'UTF8'),now()+interval '1 day') RETURNING id::text`, ws, ceiling+"-new@"+slug+".example", ceiling, "invite-"+slug+ceiling).Scan(&link); err != nil {
				t.Fatal(err)
			}
			ids[slug+"/invite-"+ceiling] = link
		}
		for _, status := range []string{"open", "closed"} {
			var contact, conversation string
			if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'C',$2) RETURNING id::text`, ws, status+"@c.example").Scan(&contact); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status) VALUES($1,$2,'S',$3) RETURNING id::text`, ws, contact, status).Scan(&conversation); err != nil {
				t.Fatal(err)
			}
			ids[slug+"/conv-"+status] = conversation
			if _, err := pool.Exec(ctx, `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,requested_by_agent_id,kind,method,route,path,body,summary,conversation_id,expires_at)
				VALUES($1,$2,'Claude','send',$3,'reply_draft','POST','/api/v1/conversations/{id}/messages','/x','{"kind":"outgoing","body":"Hello there","cc":["a@b.example"]}','Reply',$4,now()+interval '7 days')`,
				ws, token, ids[slug+"/agent"], conversation); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,requested_by_agent_id,kind,method,route,path,summary,expires_at)
			VALUES($1,$2,'Claude','send',$3,'action','GET','/api/v1/export','/api/v1/export','Export',now()+interval '7 days')`, ws, token, ids[slug+"/agent"]); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	return pool, ids
}

func rolesOf(t *testing.T, pool *pgxpool.Pool, agent string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT r.system FROM agent_roles ar JOIN roles r ON r.workspace_id=ar.workspace_id AND r.id=ar.role_id
		WHERE ar.agent_id=$1 ORDER BY 1`, agent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var systems []string
	for rows.Next() {
		var system string
		if err := rows.Scan(&system); err != nil {
			t.Fatal(err)
		}
		systems = append(systems, system)
	}
	return systems
}

func TestRolesMigrationGivesEveryMemberTheRoleTheyHad(t *testing.T) {
	pool, ids := migrateBeforeRoles(t)
	for _, slug := range []string{"one", "two"} {
		for key, want := range map[string]string{"admin": "admin", "agent": "agent", "gone": "agent"} {
			if got := rolesOf(t, pool, ids[slug+"/"+key]); !slices.Equal(got, []string{want}) {
				t.Errorf("%s/%s holds %v after the upgrade, want [%s]", slug, key, got, want)
			}
		}
		var cross int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_roles ar JOIN roles r ON r.id=ar.role_id WHERE r.workspace_id<>ar.workspace_id`).Scan(&cross); err != nil || cross != 0 {
			t.Fatalf("roles crossing workspaces: %d %v", cross, err)
		}
		for ceiling, want := range map[string]string{"admin": "admin", "agent": "agent"} {
			var system string
			if err := pool.QueryRow(context.Background(), `SELECT r.system FROM invite_roles i JOIN roles r ON r.workspace_id=i.workspace_id AND r.id=i.role_id
				WHERE i.link_id=$1 AND i.workspace_id=$2`, ids[slug+"/invite-"+ceiling], ids[slug]).Scan(&system); err != nil || system != want {
				t.Errorf("%s invite with ceiling %s gives %q (%v), want %s", slug, ceiling, system, err, want)
			}
		}
	}
}

func TestRolesMigrationNeverWidensASession(t *testing.T) {
	pool, ids := migrateBeforeRoles(t)
	ctx := context.Background()
	var system string
	if err := pool.QueryRow(ctx, `SELECT r.system FROM sessions s JOIN roles r ON r.workspace_id=s.workspace_id AND r.id=s.role_id WHERE s.id=$1`, ids["one/limited"]).Scan(&system); err != nil || system != "agent" {
		t.Fatalf("an admin's session limited to the agent role is limited to %q (%v), want the Agent role", system, err)
	}
	var legacy bool
	var scope, ceiling string
	if err := pool.QueryRow(ctx, `SELECT legacy,scope,role_ceiling FROM sessions WHERE id=$1`, ids["one/token"]).Scan(&legacy, &scope, &ceiling); err != nil {
		t.Fatal(err)
	}
	if !legacy || scope != "send" || ceiling != "agent" {
		t.Fatalf("a send token after the upgrade: legacy=%v scope=%q ceiling=%q, want legacy send with the agent ceiling", legacy, scope, ceiling)
	}
	var unlimited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE role_id IS NOT NULL AND id<>ALL($1::uuid[])`, []string{ids["one/limited"], ids["two/limited"]}).Scan(&unlimited); err != nil || unlimited != 0 {
		t.Fatalf("%d other sessions got a role limit (%v), want none", unlimited, err)
	}
}

func TestPendingRequestsCloseAndReplyDraftsBecomeAIDrafts(t *testing.T) {
	pool, ids := migrateBeforeRoles(t)
	ctx := context.Background()
	var pending, expired int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='pending'), count(*) FILTER (WHERE status='expired' AND decided_at IS NOT NULL AND reason LIKE 'Colliedesk removed agent requests%') FROM agent_requests`).Scan(&pending, &expired); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || expired != 6 {
		t.Fatalf("after the upgrade %d requests wait and %d closed with the reason, want 0 and 6", pending, expired)
	}
	for _, slug := range []string{"one", "two"} {
		var text, source, status string
		var cc []string
		var token string
		if err := pool.QueryRow(ctx, `SELECT text,source,status,cc,requested_by_token_id::text FROM ai_drafts WHERE workspace_id=$1 AND conversation_id=$2`,
			ids[slug], ids[slug+"/conv-open"]).Scan(&text, &source, &status, &cc, &token); err != nil {
			t.Fatalf("%s: the pending reply draft on an open conversation is not a draft: %v", slug, err)
		}
		if text != "Hello there" || source != "written" || status != "ready" || !slices.Equal(cc, []string{"a@b.example"}) || token != ids[slug+"/token"] {
			t.Fatalf("%s: draft = %q %s %s %v token %s", slug, text, source, status, cc, token)
		}
		var closed int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM ai_drafts WHERE conversation_id=$1`, ids[slug+"/conv-closed"]).Scan(&closed); err != nil || closed != 0 {
			t.Fatalf("%s: a closed conversation got %d drafts (%v), want none", slug, closed, err)
		}
	}
}

func TestEveryWorkspaceGetsItsSystemRoles(t *testing.T) {
	pool, ids := migrateBeforeRoles(t)
	ctx := context.Background()
	var created string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('three','Three') RETURNING id::text`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{ids["one"], ids["two"], created} {
		var admin, agent int
		var agentPermissions []string
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE system='admin' AND permissions IS NULL), count(*) FILTER (WHERE system='agent'),
			(SELECT permissions FROM roles WHERE workspace_id=$1 AND system='agent') FROM roles WHERE workspace_id=$1`, ws).Scan(&admin, &agent, &agentPermissions); err != nil {
			t.Fatal(err)
		}
		if admin != 1 || agent != 1 || len(agentPermissions) != 46 {
			t.Fatalf("workspace %s has %d Admin and %d Agent roles, Agent with %d permissions; want 1, 1 and 46", ws, admin, agent, len(agentPermissions))
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO roles(workspace_id,name,system,permissions) VALUES($1,'Second admin','admin',NULL)`, created); err == nil {
		t.Fatal("a second Admin role was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE roles SET deleted_at=now() WHERE workspace_id=$1 AND system='agent'`, created); err == nil {
		t.Fatal("the Agent role was deleted")
	}
}

// Rolling back reopens the requests the upgrade closed and drops the drafts it wrote from them, so the
// previous release finds its requests as they were and no reply a token wrote passes for an AI draft.
func TestRolesRollbackReopensRequestsAndDropsWrittenDrafts(t *testing.T) {
	pool, ids := migrateBeforeRoles(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE agent_requests SET status='expired', decided_at=now(), reason='Expired after 7 days.' WHERE workspace_id=$1 AND status='expired' AND id=(SELECT id FROM agent_requests WHERE workspace_id=$1 ORDER BY id LIMIT 1)`, ids["one"]); err != nil {
		t.Fatal(err)
	}
	// Later migrations are rolled back first, as an operator going back to the release before roles would.
	for applied := true; applied; {
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, rolesMigration).Scan(&applied); err != nil {
			t.Fatal(err)
		}
	}
	var pending, otherExpired, drafts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='pending' AND decided_at IS NULL AND reason IS NULL),
		count(*) FILTER (WHERE status='expired' AND reason='Expired after 7 days.') FROM agent_requests`).Scan(&pending, &otherExpired); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ai_drafts`).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if pending != 5 || otherExpired != 1 || drafts != 0 {
		t.Fatalf("after the rollback %d requests wait, %d closed for another reason stay closed, %d drafts remain; want 5, 1, 0", pending, otherExpired, drafts)
	}
}
