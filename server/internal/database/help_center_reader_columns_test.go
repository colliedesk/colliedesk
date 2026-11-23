package database

import (
	"context"
	"strings"
	"testing"
)

// readerColumns are the only columns the reader may read on a table it has no table-wide grant on:
// what finds the page an old helpdesk address of an imported item became, and which version an
// article's text is at, which help chat keeps with what it cites.
const readerColumns = "import_items.entity SELECT, import_items.source SELECT, import_items.source_id SELECT, " +
	"import_items.status SELECT, import_items.target_id SELECT, import_items.updated_at SELECT, " +
	"import_items.workspace_id SELECT, kb_article_versions.article_id SELECT, kb_article_versions.kind SELECT, " +
	"kb_article_versions.version SELECT, kb_article_versions.workspace_id SELECT"

func TestTheHelpCenterReaderReadsOnlyTheLedgerColumnsARedirectNeeds(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `
		SELECT c.relname || '.' || a.attname || ' ' || array_to_string(ARRAY(SELECT p FROM unnest(ARRAY['SELECT',
			'INSERT', 'UPDATE', 'REFERENCES']) p WHERE has_column_privilege($1, c.oid, a.attnum, p)
			AND NOT has_table_privilege($1, c.oid, p)), ',')
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p', 'v', 'm') AND a.attnum > 0
			AND NOT a.attisdropped AND has_column_privilege($1, c.oid, a.attnum, 'SELECT, INSERT, UPDATE, REFERENCES')
			AND NOT has_table_privilege($1, c.oid, 'SELECT, INSERT, UPDATE, REFERENCES')
		ORDER BY 1`, readerRole(t, pool))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ", ") != readerColumns {
		t.Fatalf("reader column privileges:\n%s\nwant:\n%s", strings.Join(got, "\n"), readerColumns)
	}
}
