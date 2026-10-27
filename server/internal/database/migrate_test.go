package database

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateCreatesTenantScopedTables(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name = 'workspace_id'
		ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{"agent_requests", "agents", "ai_automatic_limits", "ai_draft_sources", "ai_drafts", "ai_providers", "ai_retrieval_docs", "ai_retrieval_former_names", "ai_retrieval_pending", "ai_settings_events", "ai_slots", "ai_tasks", "ai_triage_suggestions", "ai_usage", "attachments", "autofix_connectors", "autofix_github_installations", "autofix_oauth_states", "autofix_repos", "billing_webhook_events", "channel_credentials", "channel_events", "channel_inbound_events", "channel_inbound_files", "channel_outbox", "channel_sent_ids", "channels", "contact_identities", "contact_merges", "contacts", "conversation_presence", "conversation_status_changes", "conversation_tags", "conversation_thread_keys", "conversations", "email_channels", "email_domains", "email_provider_domain_trash", "fix_attempt_events", "fix_attempts", "help_centers", "import_items", "import_jobs", "import_people", "kb_article_media", "kb_article_translations", "kb_article_versions", "kb_articles", "kb_collections", "kb_media", "kb_media_batches", "kb_media_replacements", "kb_media_uploads", "mail_image_cache", "mailbox_connections", "mailbox_oauth_states", "messages", "pairing_links", "relay_webhook_events", "results_members", "results_share_links", "results_snapshots", "results_state", "saved_replies", "saved_reply_versions", "saved_views", "sessions", "tags", "time_entries", "web_form_channels", "workspace_billing", "workspace_comp_plans", "workspace_onboarding", "workspace_retention_notices", "workspace_trial_changes"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workspace-scoped tables = %v, want %v", got, want)
	}
}

func TestMigrateCreatesNoPasswordColumns(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name ILIKE '%password%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("password columns = %d, want 0", count)
	}
}

func TestMigrateRejectsCrossWorkspaceRelationships(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var workspaceA, workspaceB, contactB string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('alpha','Alpha') RETURNING id::text`).Scan(&workspaceA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('beta','Beta') RETURNING id::text`).Scan(&workspaceB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Example','person@example.com') RETURNING id::text`, workspaceB).Scan(&contactB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) VALUES($1,$2,'Cross tenant')`, workspaceA, contactB); err == nil {
		t.Fatal("cross-workspace contact relationship was accepted")
	}
}

func TestMigrationAppliesUpDownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("first up: %v", err)
	}
	if !tableExists(t, pool, "workspaces") || !tableExists(t, pool, "channel_outbox") {
		t.Fatal("tables missing after first up")
	}
	if err := rollbackThrough(ctx, pool, "20261014101628-email-channel.up.sql"); err != nil {
		t.Fatalf("down email channel: %v", err)
	}
	if tableExists(t, pool, "email_outbox") || tableExists(t, pool, "email_channels") || !tableExists(t, pool, "workspaces") {
		t.Fatal("email channel down must remove only its own tables")
	}
	var applied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	for range applied {
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	if tableExists(t, pool, "workspaces") {
		t.Fatal("workspaces table remains after down")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if !tableExists(t, pool, "workspaces") || !tableExists(t, pool, "channel_outbox") {
		t.Fatal("tables missing after second up")
	}
}

func TestEmailChannelMigrationPreservesExistingHelpdeskData(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && entry.Name() < "20261014101628-email-channel.up.sql" {
			if err := applyMigration(ctx, pool, entry.Name()); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, agent, contact, conversation, message string
	steps := []struct {
		sql    string
		args   func() []any
		target *string
	}{
		{`INSERT INTO workspaces(slug,name) VALUES('legacy','Legacy') RETURNING id::text`, func() []any { return nil }, &workspace},
		{`INSERT INTO agents(workspace_id,email,role) VALUES($1,'admin@legacy.example','admin') RETURNING id::text`, func() []any { return []any{workspace} }, &agent},
		{`INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Old Customer','old@customer.example') RETURNING id::text`, func() []any { return []any{workspace} }, &contact},
		{`INSERT INTO conversations(workspace_id,contact_id,subject,status) VALUES($1,$2,'Old ticket','resolved') RETURNING id::text`, func() []any { return []any{workspace, contact} }, &conversation},
		{`INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body,created_at) VALUES($1,$2,$3,'outgoing','Old reply','2026-01-02T03:04:05Z') RETURNING id::text`, func() []any { return []any{workspace, conversation, agent} }, &message},
	}
	for _, step := range steps {
		if err := pool.QueryRow(ctx, step.sql, step.args()...).Scan(step.target); err != nil {
			t.Fatalf("%s: %v", step.sql, err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var body, status, createdAt string
	var emailID, token *string
	err = pool.QueryRow(ctx, `SELECT m.body,c.status,to_char(m.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS'),m.external_id,(SELECT key FROM conversation_thread_keys k WHERE k.conversation_id=c.id LIMIT 1)
		FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.id=$2`, workspace, message).Scan(&body, &status, &createdAt, &emailID, &token)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Old reply" || status != "resolved" || createdAt != "2026-01-02T03:04:05" || emailID != nil || token != nil {
		t.Fatalf("upgrade changed data: body=%q status=%q created=%q email=%v token=%v", body, status, createdAt, emailID, token)
	}
	var outbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM channel_outbox`).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("upgrade must not queue historic replies: %d %v", outbox, err)
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func rollbackLatest(ctx context.Context, pool *pgxpool.Pool) error {
	var upName string
	if err := pool.QueryRow(ctx, `SELECT name FROM schema_migrations ORDER BY applied_at DESC,name DESC LIMIT 1`).Scan(&upName); err != nil {
		return err
	}
	downName := strings.TrimSuffix(upName, ".up.sql") + ".down.sql"
	contents, err := migrations.Files.ReadFile(downName)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, string(contents)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE name=$1`, upName); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL test")
	}
	schema := "test_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(t.Name(), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return pool
}
