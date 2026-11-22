package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// helpCenterScope is how PostgreSQL prints the help center a reader transaction is on.
const helpCenterScope = `( SELECT (NULLIF(current_setting('app.help_center_id'::text, true), ''::text))::uuid AS "nullif")`

// helpCenterPolicies are the tables the public help center reads, each with the rows it shows on the
// help center in app.help_center_id, as PostgreSQL 16 to 19 print the help_center_isolation policy.
// A collection belongs to the help center its top-level collection is placed on; an article to the
// collections it sits or is listed in; translations, media, tags and votes follow their article; a
// file or image to the article or help center that shows it.
var helpCenterPolicies = map[string]string{
	"help_centers":               "(id = " + helpCenterScope + ")",
	"help_center_languages":      "(help_center_id = " + helpCenterScope + ")",
	"kb_collection_help_centers": "(help_center_id = " + helpCenterScope + ")",
	"kb_collections": "(top_collection_id IN ( SELECT kb_collection_help_centers.collection_id\n" +
		"   FROM kb_collection_help_centers))",
	"kb_collection_translations": "(collection_id IN ( SELECT kb_collections.id\n   FROM kb_collections))",
	"kb_article_collections":     "(collection_id IN ( SELECT kb_collections.id\n   FROM kb_collections))",
	"kb_articles": "((collection_id IN ( SELECT kb_collections.id\n   FROM kb_collections)) OR " +
		"(id IN ( SELECT kb_article_collections.article_id\n   FROM kb_article_collections)))",
	"kb_article_translations":  articleScope,
	"kb_article_media":         articleScope,
	"kb_article_attachments":   articleScope,
	"kb_article_tags":          articleScope,
	"kb_article_vote_resets":   articleScope,
	"kb_article_votes":         articleScope,
	"kb_article_vote_versions": articleScope,
	"kb_media": "((id IN ( SELECT kb_article_media.media_id\n   FROM kb_article_media)) OR " +
		"(id IN ( SELECT kb_article_attachments.media_id\n   FROM kb_article_attachments)) OR " +
		"(id IN ( SELECT help_centers.logo_media_id\n   FROM help_centers)) OR " +
		"(id IN ( SELECT help_centers.favicon_media_id\n   FROM help_centers)))",
}

const articleScope = "(article_id IN ( SELECT kb_articles.id\n   FROM kb_articles))"

// readerPrivileges is everything the reader may do; every other table and view of the schema gives
// it nothing, so the inbox, contacts, channels, tokens and billing are permission denied. A visitor
// records a vote; kb_public_articles and kb_public_translations stay out of reach because, read as
// the reader, they would miss the listings on other help centers.
var readerPrivileges = map[string]string{
	"kb_article_votes":                   "SELECT,INSERT,UPDATE",
	"kb_article_vote_versions":           "SELECT,INSERT,UPDATE",
	"kb_help_center_public_articles":     "SELECT",
	"kb_help_center_public_translations": "SELECT",
	"kb_collection_visibility":           "SELECT",
	"kb_published_articles":              "SELECT",
	"kb_published_translations":          "SELECT",
}

func readerPrivilegesOn(name string) string {
	if privileges, ok := readerPrivileges[name]; ok {
		return privileges
	}
	if _, ok := helpCenterPolicies[name]; ok {
		return "SELECT"
	}
	return ""
}

// readerRole is the reader's name as the migration derives it from the database name.
func readerRole(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var role string
	if err := pool.QueryRow(context.Background(), `SELECT reader_role()`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

func helpCenterIsolationPolicy(reader, scope string) string {
	return fmt.Sprintf("help_center_isolation | RESTRICTIVE | ALL | {%s} | %s | %s", reader, scope, scope)
}

// readerGaps lists every table, view and sequence of the current schema on which the reader role has
// other privileges than readerPrivilegesOn names.
func readerGaps(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	ctx := context.Background()
	reader := readerRole(t, pool)
	rows, err := pool.Query(ctx, `
		SELECT c.relname, array_to_string(ARRAY(SELECT p FROM unnest(ARRAY['SELECT', 'INSERT', 'UPDATE',
			'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER', 'USAGE']) p
			WHERE CASE WHEN c.relkind = 'S' THEN p IN ('SELECT', 'UPDATE', 'USAGE')
				AND has_sequence_privilege($1, c.oid, p)
				ELSE p <> 'USAGE' AND has_table_privilege($1, c.oid, p) END), ',')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
		ORDER BY c.relname`, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gaps []string
	for rows.Next() {
		var name, got string
		if err := rows.Scan(&name, &got); err != nil {
			t.Fatal(err)
		}
		if want := readerPrivilegesOn(name); got != want {
			gaps = append(gaps, fmt.Sprintf("%s (reader may %q, want %q)", name, got, want))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return gaps
}

func TestTheHelpCenterReaderReachesOnlyThePublicHelpCenter(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if gaps := readerGaps(t, pool); len(gaps) != 0 {
		t.Fatalf("reader privileges differ:\n%s", strings.Join(gaps, "\n"))
	}
}

func TestSecurityGuardFlagsAWidenedHelpCenterPolicy(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`ALTER POLICY help_center_isolation ON kb_articles USING (true) WITH CHECK (true)`); err != nil {
		t.Fatal(err)
	}
	_, gaps := securityGaps(t, pool)
	if len(gaps) != 1 || !strings.HasPrefix(gaps[0], "kb_articles ") {
		t.Fatalf("gaps = %v, want only kb_articles", gaps)
	}
}

func TestSecurityGuardFlagsAReaderGrantOnTheInbox(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`GRANT SELECT ON conversations TO %q`, readerRole(t, pool))); err != nil {
		t.Fatal(err)
	}
	gaps := readerGaps(t, pool)
	if len(gaps) != 1 || !strings.HasPrefix(gaps[0], "conversations ") {
		t.Fatalf("gaps = %v, want only conversations", gaps)
	}
}
