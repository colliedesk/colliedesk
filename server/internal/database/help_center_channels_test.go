package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

const helpCenterChannelsMigration = "20261122220744-help-center-channels.up.sql"

// A database that already holds channels and conversations upgrades with every row kept and no help
// center named: they all keep serving the primary. A channel then names a help center of its own
// workspace only, and only a help chat or web form names one.
func TestChannelsAndConversationsUpgradeToNameNoHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := rollbackThrough(ctx, pool, helpCenterChannelsMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	for _, ws := range []string{ids.a, ids.b} {
		if _, err := pool.Exec(ctx, `INSERT INTO channels(workspace_id,type,name) VALUES($1,'help_chat','Chat'),($1,'email','Mail')`, ws); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	var channels, conversations, named int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channels), (SELECT count(*) FROM conversations),
		(SELECT count(*) FROM channels WHERE help_center_id IS NOT NULL) + (SELECT count(*) FROM conversations WHERE help_center_id IS NOT NULL)`).
		Scan(&channels, &conversations, &named); err != nil {
		t.Fatal(err)
	}
	if channels != 4 || conversations != 2 || named != 0 {
		t.Fatalf("after the upgrade: %d channels, %d conversations, %d naming a help center; want 4, 2, 0", channels, conversations, named)
	}
	var second string
	if err := pool.QueryRow(ctx, `INSERT INTO help_centers(workspace_id,handle,primary_help_center,title) VALUES($1,'second',false,'Second') RETURNING id::text`, ids.a).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET help_center_id=$2 WHERE workspace_id=$1 AND type='help_chat'`, ids.a, second); err != nil {
		t.Fatalf("a help chat naming its own workspace's help center: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE conversations SET help_center_id=$2 WHERE workspace_id=$1`, ids.a, second); err != nil {
		t.Fatalf("a conversation naming its own workspace's help center: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET help_center_id=$2 WHERE workspace_id=$1 AND type='help_chat'`, ids.b, second); err == nil {
		t.Fatal("a channel named another workspace's help center")
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET help_center_id=$2 WHERE workspace_id=$1 AND type='email'`, ids.a, second); err == nil {
		t.Fatal("an email channel named a help center")
	}
	if _, err := pool.Exec(ctx, `UPDATE conversations SET help_center_id=$2 WHERE workspace_id=$1`, ids.b, second); err == nil {
		t.Fatal("a conversation named another workspace's help center")
	}
	if err := rollbackThrough(ctx, pool, helpCenterChannelsMigration); err != nil {
		t.Fatalf("down with help centers named: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channels), (SELECT count(*) FROM conversations)`).Scan(&channels, &conversations); err != nil || channels != 4 || conversations != 2 {
		t.Fatalf("after the down migration: %d channels, %d conversations, err=%v; want 4, 2", channels, conversations, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up a third time: %v", err)
	}
}

// helpChatArticles returns the ids of the articles the chat view returns to a reader of helpCenter,
// with help chat on or off.
func helpChatArticles(t *testing.T, db *DB, workspace, helpCenter string, chat bool) map[string]bool {
	t.Helper()
	ctx := context.Background()
	seen := map[string]bool{}
	run := db.InHelpCenter
	if chat {
		run = db.InHelpChat
	}
	err := run(ctx, workspace, helpCenter, func(tx pgx.Tx) error {
		// The reader's own policies on kb_articles keep the help center's articles.
		rows, err := tx.Query(ctx, `SELECT a.id::text FROM kb_help_center_chat_articles v JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.id`)
		if err != nil {
			return err
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, id := range got {
			seen[id] = true
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return seen
}

// Help chat on a help center reads the articles that help center shows, plus those kept for help
// chat, and nothing of another help center or of a collection placed on none. The same reader
// without help chat on (a public page) reads none of them, and the public view never returns an
// article kept for help chat.
func TestHelpChatReadsOnlyItsHelpCenterAndKeepsChatArticlesOffPublicPages(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	for _, statement := range []string{
		`UPDATE kb_articles SET status='published', published_at=now()`,
		`UPDATE kb_article_translations SET status='published', published_at=now()`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_articles SET visibility='help_chat' WHERE id=$1`, s.secondOnly); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	onSecond := helpChatArticles(t, db, s.ids.a, s.second, true)
	if !onSecond[s.secondOnly] || !onSecond[s.first] || onSecond[s.private] || len(onSecond) != 2 {
		t.Errorf("help chat on the second help center reads %v; want the help-chat-only article and article 1 only", onSecond)
	}
	onPrimary := helpChatArticles(t, db, s.ids.a, s.primary, true)
	if !onPrimary[s.first] || onPrimary[s.secondOnly] || onPrimary[s.private] || len(onPrimary) != 1 {
		t.Errorf("help chat on the primary reads %v; want article 1 only", onPrimary)
	}
	if public := helpChatArticles(t, db, s.ids.a, s.second, false); len(public) != 0 {
		t.Errorf("a public page of the second help center reads %d chat articles; want none", len(public))
	}
	var public, translations int
	err := db.InHelpChat(ctx, s.ids.a, s.second, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_help_center_public_articles WHERE id=$1`, s.secondOnly).Scan(&public); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM kb_help_center_chat_translations WHERE article_id=$1`, s.secondOnly).Scan(&translations)
	})
	if err != nil {
		t.Fatal(err)
	}
	if public != 0 {
		t.Error("the public view returned an article kept for help chat")
	}
	if translations != 1 {
		t.Errorf("help chat reads %d translations of its help-chat-only article; want 1", translations)
	}
	for _, article := range []string{s.secondOnly, s.private} {
		if _, err := pool.Exec(ctx, `INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,status)
			VALUES($1,$2,100,'edit','t','s','b','published')`, s.ids.a, article); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO ai_retrieval_docs(workspace_id,kb_article_id,title,body,folded,tokens)
			VALUES($1,$2,'t','b',to_tsvector('simple','b'),1)`, s.ids.a, article); err != nil {
			t.Fatal(err)
		}
	}
	for helpCenter, want := range map[string]int{s.primary: 0, s.second: 1} {
		var versions, docs int
		err := db.InHelpChat(ctx, s.ids.a, helpCenter, func(tx pgx.Tx) error {
			// Deliberately unfiltered by help center: row-level security alone must scope it.
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_article_versions WHERE version=100`).Scan(&versions); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM ai_retrieval_docs WHERE title='t'`).Scan(&docs)
		})
		if err != nil {
			t.Fatal(err)
		}
		if versions != want || docs != want {
			t.Errorf("help chat on %s reads %d versions and %d retrieval rows; want %d of each (the unplaced article never)", helpCenter, versions, docs, want)
		}
	}
}
