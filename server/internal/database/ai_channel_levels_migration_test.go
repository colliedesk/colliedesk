package database

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const aiChannelLevelsMigration = "20261029120000-ai-channel-levels.up.sql"

// aiTasksForANewConversation starts a conversation in the workspace with one customer message and
// returns the kinds of the automatic tasks it queued, sorted.
func aiTasksForANewConversation(t *testing.T, pool *pgxpool.Pool, workspaceID string) []string {
	t.Helper()
	ctx := context.Background()
	var conversationID string
	if err := pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ann','ann-'||gen_random_uuid()||'@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject,channel) SELECT $1,id,'Printing fails','email' FROM c RETURNING id::text`, workspaceID).Scan(&conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming','It fails')`, workspaceID, conversationID); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT kind FROM ai_tasks WHERE workspace_id=$1 AND conversation_id=$2 AND automatic ORDER BY kind`, workspaceID, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	kinds := []string{}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
	}
	return kinds
}

// The levels per channel keep what every workspace had: the same customer message queues the same
// automatic work before and after the upgrade, and the old switches become the levels they meant.
func TestAIChannelLevelsMigrationKeepsWhatEachWorkspaceRanByItself(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	migrateBefore(t, pool, aiChannelLevelsMigration)
	type state struct {
		slot               string
		enabled, automatic bool
		parts              []string
		level              string
	}
	cases := map[string]state{
		"triage-on":   {slot: "triage", enabled: true, level: "suggest"},
		"triage-auto": {slot: "triage", enabled: true, automatic: true, parts: []string{"tags"}, level: "auto"},
		"draft-on":    {slot: "draft", enabled: true, level: "off"},
		"draft-auto":  {slot: "draft", enabled: true, automatic: true, level: "suggest"},
		"triage-off":  {slot: "triage", level: "off"},
	}
	workspaces := map[string]string{}
	before := map[string][]string{}
	for slug, s := range cases {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(&id); err != nil {
			t.Fatal(err)
		}
		parts := s.parts
		if parts == nil {
			parts = []string{}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,automatic,auto_parts,consented_at)
			VALUES($1,$2,true,'model-1',$3,$4,$5,CASE WHEN $3 THEN now() END)`, id, s.slot, s.enabled, s.automatic, parts); err != nil {
			t.Fatal(err)
		}
		workspaces[slug] = id
		before[slug] = aiTasksForANewConversation(t, pool, id)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	for slug, s := range cases {
		var level string
		if err := pool.QueryRow(ctx, `SELECT default_level FROM ai_slots WHERE workspace_id=$1 AND slot=$2`, workspaces[slug], s.slot).Scan(&level); err != nil {
			t.Fatal(err)
		}
		if level != s.level {
			t.Errorf("%s: default_level %s, want %s", slug, level, s.level)
		}
		after := aiTasksForANewConversation(t, pool, workspaces[slug])
		if !slices.Equal(before[slug], after) {
			t.Errorf("%s: a customer message queued %v before the upgrade and %v after", slug, before[slug], after)
		}
	}
	if !slices.Equal(before["triage-on"], []string{"triage"}) || !slices.Equal(before["draft-auto"], []string{"draft"}) || len(before["draft-on"]) != 0 {
		t.Fatalf("the seed does not exercise the triggers: %v", before)
	}
}

// A level set on a channel's scope wins over the channel's, which wins over the workspace's default;
// a feature the workspace has not turned on is off everywhere. One-to-one does not read the group's
// row (the function's parameters are not its columns).
func TestAILevelReadsTheScopeThenTheChannelThenTheWorkspace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var workspaceID, channelID, other string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('levels','levels') RETURNING id::text`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO channels(workspace_id,type,name) VALUES($1,'email','Support') RETURNING id::text`, workspaceID).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO channels(workspace_id,type,name) VALUES($1,'email','Sales') RETURNING id::text`, workspaceID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	level := func(channel *string, scope *string, slot string) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT ai_level($1,$2,$3,$4)`, workspaceID, channel, scope, slot).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	group, direct := "group", "direct"
	if got := level(&channelID, &group, "triage"); got != "off" {
		t.Fatalf("no slot: %s", got)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,default_level,consented_at) VALUES($1,'triage',true,'m',false,'off',NULL)`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level) VALUES($1,$2,'group','triage','auto'),($1,$2,'','triage','off')`, workspaceID, channelID); err != nil {
		t.Fatal(err)
	}
	if got := level(&channelID, &group, "triage"); got != "off" {
		t.Fatalf("a channel's auto while the workspace has triage off: %s", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE ai_slots SET enabled=true,default_level='suggest',consented_at=now() WHERE workspace_id=$1`, workspaceID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		channel, scope *string
		want           string
	}{{&channelID, &group, "auto"}, {&channelID, &direct, "off"}, {&channelID, nil, "off"}, {&other, &group, "suggest"}, {nil, nil, "suggest"}} {
		if got := level(c.channel, c.scope, "triage"); got != c.want {
			t.Errorf("channel %v scope %v: %s, want %s", c.channel != nil, c.scope, got, c.want)
		}
	}
	for _, refused := range []string{
		`INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level) VALUES($1,$2,'','draft','auto')`,
		`INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level) VALUES($1,$2,'','intake','suggest')`,
		`INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level,options) VALUES($1,$2,'','triage','auto','{"a":1}')`,
	} {
		if _, err := pool.Exec(ctx, refused, workspaceID, channelID); err == nil {
			t.Errorf("accepted: %s", refused)
		}
	}
	for _, refused := range []string{
		`UPDATE ai_slots SET default_level='auto' WHERE workspace_id=$1 AND slot='triage'`,
		`INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,default_level,consented_at) VALUES($1,'draft',true,'m',true,'auto',now())`,
		`INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,default_level,consented_at) VALUES($1,'intake',true,'m',true,'auto',now())`,
		`INSERT INTO ai_slots(workspace_id,slot,enabled,default_level) VALUES($1,'draft',false,'suggest')`,
	} {
		if _, err := pool.Exec(ctx, refused, workspaceID); err == nil {
			t.Errorf("accepted: %s", refused)
		}
	}
}
