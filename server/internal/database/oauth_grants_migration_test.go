package database

import (
	"context"
	"testing"
)

const oauthGrantsMigration = "20261106090000-oauth-grants.up.sql"

// Rolling back keeps an AI app's grant row, because a note it wrote names it: the previous release reads it
// as a revoked token, and the note still says which app wrote it.
func TestOAuthGrantsRollbackKeepsTheNotesAGrantWroteAttributed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT set_config('app.privileged','on',false)`); err != nil {
		t.Fatal(err)
	}
	var workspace, agent, grant, message string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('rollback','Rollback') RETURNING id::text`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'ada@rollback.example','admin') RETURNING id::text`, workspace).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at,oauth_client_id,oauth_redirect_uri,oauth_resource,oauth_connected_at)
		VALUES($1,$2,convert_to('grant','UTF8'),'admin','oauth','Claude Code',now()+interval '30 days','https://claude.ai/cc.json','http://localhost/callback','https://desk.example/mcp',now())
		RETURNING id::text`, workspace, agent).Scan(&grant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO oauth_tokens(workspace_id,session_id,kind,hash,created_at,expires_at) VALUES($1,$2,'access',convert_to('access','UTF8'),now(),now()+interval '1 hour')`, workspace, grant); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'C','c@rollback.example') RETURNING id),
		v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'S' FROM c RETURNING id)
		INSERT INTO messages(workspace_id,conversation_id,kind,body,author_agent_id,agent_token_id) SELECT $1,id,'note','Triaged',$2,$3 FROM v RETURNING id::text`,
		workspace, agent, grant).Scan(&message); err != nil {
		t.Fatal(err)
	}
	for applied := true; applied; {
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, oauthGrantsMigration).Scan(&applied); err != nil {
			t.Fatal(err)
		}
	}
	var scope string
	var revoked bool
	if err := pool.QueryRow(ctx, `SELECT scope, revoked_at IS NOT NULL FROM sessions WHERE id=$1`, grant).Scan(&scope, &revoked); err != nil {
		t.Fatalf("the grant row is gone after the rollback: %v", err)
	}
	if scope != "read" || !revoked {
		t.Fatalf("after the rollback the grant is scope %q revoked=%v, want a revoked read token", scope, revoked)
	}
	var attributed string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(agent_token_id::text,'') FROM messages WHERE id=$1`, message).Scan(&attributed); err != nil || attributed != grant {
		t.Fatalf("the note names token %q after the rollback (err %v), want the grant %s", attributed, err, grant)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade again after the rollback: %v", err)
	}
}
