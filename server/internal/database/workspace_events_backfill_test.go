package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const workspaceMilestonesMigration = "20261114180059-workspace-milestones.up.sql"

// milestoneFixture is a desk as it stands before the log exists: one workspace (acme) with history of
// every kind the backfill reads and every kind it must leave out, one (beta) whose only import ran from
// the command line, one (cobalt) that did nothing. Every time is written out by hand.
const milestoneFixture = `
INSERT INTO workspaces(id, slug, name, created_at, trial_ends_at) VALUES
	(md5('ws-acme')::uuid, 'acme', 'Acme', '2026-09-01T10:00:00Z', '2026-09-15T10:00:00Z'),
	(md5('ws-beta')::uuid, 'beta', 'Beta', '2026-09-10T00:00:00Z', '2026-09-24T00:00:00Z'),
	(md5('ws-cobalt')::uuid, 'cobalt', 'Cobalt', '2026-09-12T00:00:00Z', '2026-09-26T00:00:00Z'),
	(md5('ws-delta')::uuid, 'delta', 'Delta', '2026-09-13T00:00:00Z', '2026-09-27T00:00:00Z'),
	(md5('ws-epsilon')::uuid, 'epsilon', 'Epsilon', '2026-09-14T00:00:00Z', '2026-09-28T00:00:00Z'),
	(md5('ws-zeta')::uuid, 'zeta', 'Zeta', '2026-09-02T00:00:00Z', '2026-09-16T00:00:00Z');
INSERT INTO agents(id, workspace_id, email, role) VALUES
	(md5('agent-acme-1')::uuid, md5('ws-acme')::uuid, 'admin@acme.example', 'admin');
INSERT INTO contacts(id, workspace_id, name, email) VALUES
	(md5('contact-acme-1')::uuid, md5('ws-acme')::uuid, 'Customer', 'cust@customer.example'),
	(md5('contact-acme-2')::uuid, md5('ws-acme')::uuid, 'Admin', 'Admin@Acme.example'),
	(md5('contact-acme-3')::uuid, md5('ws-acme')::uuid, 'Gmail Team', 'forwarding-noreply@google.com'),
	(md5('contact-acme-4')::uuid, md5('ws-acme')::uuid, 'Spammer', 'offers@spam.example');
INSERT INTO channels(id, workspace_id, type, name) VALUES (md5('channel-acme-1')::uuid, md5('ws-acme')::uuid, 'email', 'Email');
INSERT INTO conversations(id, workspace_id, contact_id, subject, channel_id) VALUES
	(md5('conv-acme-1')::uuid, md5('ws-acme')::uuid, md5('contact-acme-1')::uuid, 'Order', md5('channel-acme-1')::uuid),
	(md5('conv-acme-2')::uuid, md5('ws-acme')::uuid, md5('contact-acme-2')::uuid, 'Test message from Colliedesk', md5('channel-acme-1')::uuid),
	(md5('conv-acme-3')::uuid, md5('ws-acme')::uuid, md5('contact-acme-1')::uuid, 'Imported', NULL),
	(md5('conv-acme-4')::uuid, md5('ws-acme')::uuid, md5('contact-acme-3')::uuid, 'Gmail Forwarding Confirmation', md5('channel-acme-1')::uuid);
INSERT INTO conversations(id, workspace_id, contact_id, subject, channel_id, status) VALUES
	(md5('conv-acme-5')::uuid, md5('ws-acme')::uuid, md5('contact-acme-4')::uuid, 'Cheap offers', md5('channel-acme-1')::uuid, 'spam');
INSERT INTO messages(id, workspace_id, conversation_id, kind, body, created_at, channel_id, origin, email_meta, author_agent_id, notice_kind) VALUES
	(md5('msg-acme-imported-in')::uuid, md5('ws-acme')::uuid, md5('conv-acme-3')::uuid, 'incoming', 'old question', '2026-08-01T00:00:00Z', md5('channel-acme-1')::uuid, '{"system":"freshdesk"}', '{"from":"cust@customer.example","authentication":"none"}', NULL, NULL),
	(md5('msg-acme-imported-out')::uuid, md5('ws-acme')::uuid, md5('conv-acme-3')::uuid, 'outgoing', 'old answer', '2026-08-02T00:00:00Z', NULL, '{"system":"freshdesk"}', NULL, md5('agent-acme-1')::uuid, NULL),
	(md5('msg-acme-test')::uuid, md5('ws-acme')::uuid, md5('conv-acme-2')::uuid, 'incoming', 'This is a test message', '2026-09-01T10:05:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"admin@acme.example","authentication":"pass"}', NULL, NULL),
	(md5('msg-acme-test-answer')::uuid, md5('ws-acme')::uuid, md5('conv-acme-2')::uuid, 'outgoing', 'It works', '2026-09-01T10:06:00Z', md5('channel-acme-1')::uuid, NULL, NULL, md5('agent-acme-1')::uuid, NULL),
	(md5('msg-acme-gmail')::uuid, md5('ws-acme')::uuid, md5('conv-acme-4')::uuid, 'incoming', 'Confirmation code: 1', '2026-09-01T10:07:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"forwarding-noreply@google.com","authentication":"pass"}', NULL, NULL),
	(md5('msg-acme-forged')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'incoming', 'forged', '2026-09-01T10:08:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"cust@customer.example","authentication":"fail"}', NULL, NULL),
	(md5('msg-acme-spam')::uuid, md5('ws-acme')::uuid, md5('conv-acme-5')::uuid, 'incoming', 'Buy now', '2026-09-01T10:09:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"offers@spam.example","authentication":"pass"}', NULL, NULL),
	(md5('msg-acme-live-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'incoming', 'Where is my order', '2026-09-02T08:00:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"cust@customer.example","authentication":"pass"}', NULL, NULL),
	(md5('msg-acme-notice')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'outgoing', 'We got your message', '2026-09-02T08:01:00Z', md5('channel-acme-1')::uuid, NULL, NULL, NULL, 'received'),
	(md5('msg-acme-note')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'note', 'checking', '2026-09-02T08:30:00Z', NULL, NULL, NULL, md5('agent-acme-1')::uuid, NULL),
	(md5('msg-acme-reply-failed')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'outgoing', 'Saved, channel was off', '2026-09-02T08:50:00Z', md5('channel-acme-1')::uuid, NULL, NULL, md5('agent-acme-1')::uuid, NULL),
	(md5('msg-acme-reply')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'outgoing', 'It ships today', '2026-09-02T09:00:00Z', md5('channel-acme-1')::uuid, NULL, NULL, md5('agent-acme-1')::uuid, NULL),
	(md5('msg-acme-live-2')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'incoming', 'Thanks', '2026-09-03T08:00:00Z', md5('channel-acme-1')::uuid, NULL, '{"from":"cust@customer.example","authentication":"pass"}', NULL, NULL);
INSERT INTO channel_outbox(workspace_id, message_id, status, last_error, last_error_code, created_at, recipients, channel_type, channel_id) VALUES
	(md5('ws-acme')::uuid, md5('msg-acme-reply-failed')::uuid, 'failed', 'Email is off', 'channel_off', '2026-09-02T08:50:00Z', 1, 'email', md5('channel-acme-1')::uuid);
INSERT INTO import_jobs(workspace_id, kind, source, domain, account_source, state, phase, status, created_at, finished_at) VALUES
	(md5('ws-acme')::uuid, 'import', 'freshdesk', 'acme.freshdesk.example', 'freshdesk:account:7', 'finished', 'full', 'FAILED', '2026-09-01T11:00:00Z', '2026-09-01T12:00:00Z'),
	(md5('ws-acme')::uuid, 'import', 'freshdesk', 'acme.freshdesk.example', 'freshdesk:account:7', 'finished', 'full', 'OK', '2026-09-01T12:30:00Z', '2026-09-01T13:00:00Z');
INSERT INTO results_state(workspace_id, baseline_source, baseline_anchor_at, baseline_import_status) VALUES
	(md5('ws-acme')::uuid, 'freshdesk:account:7', '2026-09-01T13:00:01Z', 'OK'),
	(md5('ws-beta')::uuid, 'helpscout:account:9', '2026-09-11T00:00:00Z', 'OK');
INSERT INTO pairing_links(id, workspace_id, agent_id, email, kind, role_ceiling, token_hash, created_at, expires_at) VALUES
	(md5('link-acme-1')::uuid, md5('ws-acme')::uuid, md5('agent-acme-1')::uuid, 'admin@acme.example', 'pairing', 'admin', 'h1', '2026-09-01T10:00:00Z', '2026-09-02T10:00:00Z'),
	(md5('link-acme-2')::uuid, md5('ws-acme')::uuid, md5('agent-acme-1')::uuid, 'admin@acme.example', 'invite', 'admin', 'h2', '2026-09-04T10:00:00Z', '2026-09-11T10:00:00Z'),
	(md5('link-acme-3')::uuid, md5('ws-acme')::uuid, NULL, 'new@acme.example', 'invite', 'agent', 'h3', '2026-09-05T10:00:00Z', '2026-09-12T10:00:00Z');
INSERT INTO workspace_billing(workspace_id, polar_subscription_id, plan, polar_customer_id, status, cancel_at_period_end, ended_at, source_modified_at) VALUES
	(md5('ws-acme')::uuid, 'sub_acme', 'team', 'cus_acme', 'canceled', true, '2026-09-20T10:00:00Z', '2026-09-20T10:00:00Z'),
	(md5('ws-delta')::uuid, 'sub_delta', 'team', 'cus_delta', 'incomplete', false, NULL, '2026-09-30T00:00:00Z'),
	(md5('ws-epsilon')::uuid, 'sub_epsilon', 'team', 'cus_epsilon', 'active', false, NULL, '2026-10-05T00:00:00Z'),
	(md5('ws-zeta')::uuid, 'sub_zeta', 'team', 'cus_zeta', 'canceled', false, '2026-09-28T00:00:00Z', '2026-09-28T00:00:00Z');
INSERT INTO billing_webhook_events(workspace_id, webhook_id, event_type, received_at) VALUES
	(md5('ws-acme')::uuid, 'w1', 'subscription.created', '2026-09-06T09:59:00Z'),
	(md5('ws-acme')::uuid, 'w2', 'subscription.active', '2026-09-06T10:00:00Z'),
	(md5('ws-acme')::uuid, 'w3', 'subscription.canceled', '2026-09-20T10:00:00Z'),
	(md5('ws-acme')::uuid, 'w4', 'subscription.active', '2026-10-01T10:00:00Z'),
	(md5('ws-delta')::uuid, 'w5', 'subscription.active', '2026-09-30T00:00:00Z'),
	(md5('ws-epsilon')::uuid, 'w6', 'reconcile', '2026-10-05T00:00:00Z'),
	(md5('ws-zeta')::uuid, 'w7', 'subscription.active', '2026-09-14T00:00:00Z'),
	(md5('ws-zeta')::uuid, 'w8', 'subscription.revoked', '2026-09-28T00:00:00Z');
`

func TestWorkspaceMilestonesBackfillReadsEachFactOnceFromTheExistingRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, workspaceMilestonesMigration); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, milestoneFixture)
		return err
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT w.slug||' '||e.kind||' '||to_char(e.at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')||' '||e.source||' '||e.detail::text
		FROM workspace_events e JOIN workspaces w ON w.id=e.workspace_id ORDER BY w.slug, e.at, e.kind`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`acme signed_up 2026-09-01T10:00:00Z backfill {}`,
		`acme first_customer_message 2026-09-01T10:09:00Z backfill {"channel_id": "` + md5UUID(t, pool, "channel-acme-1") + `", "message_id": "` + md5UUID(t, pool, "msg-acme-spam") + `", "channel_type": "email", "conversation_id": "` + md5UUID(t, pool, "conv-acme-5") + `"}`,
		`acme import_finished 2026-09-01T13:00:00Z backfill {"importer": "freshdesk"}`,
		`acme first_reply 2026-09-02T09:00:00Z backfill {"message_id": "` + md5UUID(t, pool, "msg-acme-reply") + `", "conversation_id": "` + md5UUID(t, pool, "conv-acme-1") + `"}`,
		`acme teammate_invited 2026-09-05T10:00:00Z backfill {"invite_id": "` + md5UUID(t, pool, "link-acme-3") + `"}`,
		`acme paid 2026-09-06T10:00:00Z backfill {}`,
		`acme cancelled 2026-09-20T10:00:00Z backfill {}`,
		`beta signed_up 2026-09-10T00:00:00Z backfill {}`,
		`beta import_finished 2026-09-11T00:00:00Z backfill {"importer": "helpscout", "approximate": true}`,
		`cobalt signed_up 2026-09-12T00:00:00Z backfill {}`,
		`delta signed_up 2026-09-13T00:00:00Z backfill {}`,
		`epsilon signed_up 2026-09-14T00:00:00Z backfill {}`,
		`epsilon paid 2026-10-05T00:00:00Z backfill {}`,
		`zeta signed_up 2026-09-02T00:00:00Z backfill {}`,
		`zeta paid 2026-09-14T00:00:00Z backfill {}`,
		`zeta cancelled 2026-09-28T00:00:00Z backfill {}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("backfilled events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	var view string
	if err := pool.QueryRow(ctx, `SELECT concat_ws(' ', signed_up_at, first_customer_message_at, import_finished_at, first_reply_at,
		teammate_invited_at, coalesce(trial_ended_at::text, '-'), paid_at, cancelled_at) FROM workspace_milestones WHERE workspace_id=md5('ws-acme')::uuid`).Scan(&view); err != nil {
		t.Fatal(err)
	}
	if view != "2026-09-01 10:00:00+00 2026-09-01 10:09:00+00 2026-09-01 13:00:00+00 2026-09-02 09:00:00+00 2026-09-05 10:00:00+00 - 2026-09-06 10:00:00+00 2026-09-20 10:00:00+00" {
		t.Fatalf("acme in the view = %q", view)
	}
}

func TestWorkspaceMilestonesDownRemovesEverythingAndUpRunsAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, workspaceMilestonesMigration); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace
			AND (relname LIKE 'workspace\_events%' OR relname='workspace_milestones'))
		+ (SELECT count(*) FROM pg_proc WHERE pronamespace=current_schema()::regnamespace AND proname='workspace_events_append_only')`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, pool, "workspace_events") || tableExists(t, pool, "workspace_milestones") || left != 0 {
		t.Fatalf("down left the log behind (%d objects)", left)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func md5UUID(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, seed string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT md5($1)::uuid::text`, seed).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
