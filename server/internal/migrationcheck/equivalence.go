package migrationcheck

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration is one up migration of a chain: its file name and its SQL.
type Migration struct {
	Name string
	SQL  string
}

// Equivalent applies two chains of up migrations, each into a new schema of the database at
// databaseURL, and fails at the first migration after which the two schemas differ: tables, columns,
// constraints, indexes, policies, grants, functions, triggers, views, types, comments and rows.
//
// Both chains run in name order, as the server runs them. After every name both chains have, the
// schemas must match, except that the after chain may already have a function the before chain
// defines later (a rewrite may move a helper earlier so older migrations can call it). After the
// last migration they must match exactly. Matching after every step is what lets a database that
// applied part of the old chain finish with the new one.
func Equivalent(ctx context.Context, databaseURL string, before, after []Migration) error {
	suffix := fmt.Sprint(time.Now().UnixNano())
	beforeChain, err := newChainSchema(ctx, databaseURL, "migration_before_"+suffix)
	if err != nil {
		return err
	}
	defer beforeChain.drop(ctx)
	afterChain, err := newChainSchema(ctx, databaseURL, "migration_after_"+suffix)
	if err != nil {
		return err
	}
	defer afterChain.drop(ctx)

	beforeByName := byName(before)
	afterByName := byName(after)
	var names []string
	for name := range beforeByName {
		names = append(names, name)
	}
	for name := range afterByName {
		if _, ok := beforeByName[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		beforeSQL, inBefore := beforeByName[name]
		afterSQL, inAfter := afterByName[name]
		if inBefore {
			if err := beforeChain.apply(ctx, name, beforeSQL); err != nil {
				return fmt.Errorf("before chain: %w", err)
			}
		}
		if inAfter {
			if err := afterChain.apply(ctx, name, afterSQL); err != nil {
				return fmt.Errorf("after chain: %w", err)
			}
		}
		if !inBefore || !inAfter {
			continue
		}
		if err := compareChains(ctx, beforeChain, afterChain, true); err != nil {
			return fmt.Errorf("after %s the schemas differ:\n%w", name, err)
		}
	}
	if err := compareChains(ctx, beforeChain, afterChain, false); err != nil {
		return fmt.Errorf("after the last migration the schemas differ:\n%w", err)
	}
	return nil
}

func byName(chain []Migration) map[string]string {
	named := make(map[string]string, len(chain))
	for _, migration := range chain {
		named[migration.Name] = migration.SQL
	}
	return named
}

type chainSchema struct {
	name  string
	admin *pgxpool.Pool
	pool  *pgxpool.Pool
}

func newChainSchema(ctx context.Context, databaseURL, name string) (*chainSchema, error) {
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close()
		return nil, fmt.Errorf("create schema %s: %w", name, err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		admin.Close()
		return nil, err
	}
	config.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		return nil, err
	}
	chain := &chainSchema{name: name, admin: admin, pool: pool}
	// The server's ledger; the row-level security migration revokes it from the app role.
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
		name text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		chain.drop(ctx)
		return nil, fmt.Errorf("create the migration ledger in %s: %w", name, err)
	}
	return chain, nil
}

func (c *chainSchema) drop(ctx context.Context) {
	c.pool.Close()
	_, _ = c.admin.Exec(context.WithoutCancel(ctx), "DROP SCHEMA "+pgx.Identifier{c.name}.Sanitize()+" CASCADE")
	c.admin.Close()
}

// apply runs a migration the way the server does: in one transaction marked privileged.
func (c *chainSchema) apply(ctx context.Context, name, sql string) error {
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.privileged', 'on', true)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name)
		return err
	})
}

func compareChains(ctx context.Context, before, after *chainSchema, allowEarlierFunctions bool) error {
	beforeLines, err := before.snapshot(ctx)
	if err != nil {
		return err
	}
	afterLines, err := after.snapshot(ctx)
	if err != nil {
		return err
	}
	inBefore := make(map[string]bool, len(beforeLines))
	for _, line := range beforeLines {
		inBefore[line] = true
	}
	inAfter := make(map[string]bool, len(afterLines))
	for _, line := range afterLines {
		inAfter[line] = true
	}
	beforeFunctions := make(map[string]bool)
	for _, line := range beforeLines {
		if strings.HasPrefix(line, "function ") {
			beforeFunctions[functionSignature(line)] = true
		}
	}
	var differences []string
	for _, line := range beforeLines {
		if !inAfter[line] {
			differences = append(differences, "- "+line)
		}
	}
	for _, line := range afterLines {
		if inBefore[line] {
			continue
		}
		if allowEarlierFunctions && strings.HasPrefix(line, "function ") && !beforeFunctions[functionSignature(line)] {
			continue
		}
		differences = append(differences, "+ "+line)
	}
	if len(differences) == 0 {
		return nil
	}
	const shown = 20
	var report strings.Builder
	for i, difference := range differences {
		if i == shown {
			fmt.Fprintf(&report, "  ... and %d more\n", len(differences)-shown)
			break
		}
		if len(difference) > 400 {
			difference = difference[:400] + "..."
		}
		report.WriteString("  " + difference + "\n")
	}
	return fmt.Errorf("%s(- only before, + only after)", report.String())
}

// functionSignature is "function name(arguments)", the part of a function line before its body.
func functionSignature(line string) string {
	if end := strings.Index(line, ") "); end >= 0 {
		return line[:end+1]
	}
	return line
}

// catalogQueries each return one text line per object. $1 is the schema; names print unqualified
// because the connection's search_path is the schema.
var catalogQueries = []string{
	`SELECT format('relation %s kind=%s rls=%s force=%s acl=%s', c.relname, c.relkind, c.relrowsecurity,
		c.relforcerowsecurity, (SELECT string_agg(a::text, ',' ORDER BY a::text) FROM unnest(c.relacl) a))
	FROM pg_class c WHERE c.relnamespace = $1::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')`,
	`SELECT format('column %s.%s position=%s %s notnull=%s default=%s identity=%s generated=%s collation=%s',
		c.relname, a.attname, a.attnum, format_type(a.atttypid, a.atttypmod), a.attnotnull,
		pg_get_expr(d.adbin, d.adrelid), a.attidentity, a.attgenerated, a.attcollation::regcollation)
	FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
	LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
	WHERE c.relnamespace = $1::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f') AND a.attnum > 0
		AND NOT a.attisdropped`,
	`SELECT format('constraint %s.%s %s deferrable=%s deferred=%s', coalesce(c.relname, t.typname), k.conname,
		pg_get_constraintdef(k.oid), k.condeferrable, k.condeferred)
	FROM pg_constraint k LEFT JOIN pg_class c ON c.oid = k.conrelid LEFT JOIN pg_type t ON t.oid = k.contypid
	WHERE k.connamespace = $1::regnamespace`,
	`SELECT format('index %s', pg_get_indexdef(i.indexrelid))
	FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relnamespace = $1::regnamespace`,
	`SELECT format('policy %s.%s %s %s roles=%s using=%s check=%s', tablename, policyname, permissive, cmd,
		roles, qual, with_check)
	FROM pg_policies WHERE schemaname = $1`,
	`SELECT format('function %s(%s) %s acl=%s', p.proname, pg_get_function_identity_arguments(p.oid),
		pg_get_functiondef(p.oid), (SELECT string_agg(a::text, ',' ORDER BY a::text) FROM unnest(p.proacl) a))
	FROM pg_proc p WHERE p.pronamespace = $1::regnamespace AND p.prokind IN ('f', 'p')`,
	`SELECT format('trigger %s enabled=%s', pg_get_triggerdef(t.oid), t.tgenabled)
	FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
	WHERE c.relnamespace = $1::regnamespace AND NOT t.tgisinternal`,
	`SELECT format('view %s %s', c.relname, pg_get_viewdef(c.oid))
	FROM pg_class c WHERE c.relnamespace = $1::regnamespace AND c.relkind IN ('v', 'm')`,
	`SELECT format('sequence %s type=%s start=%s increment=%s min=%s max=%s cycle=%s owned=%s',
		c.relname, format_type(s.seqtypid, NULL), s.seqstart, s.seqincrement, s.seqmin, s.seqmax, s.seqcycle,
		(SELECT d.refobjid::regclass || '.' || d.refobjsubid FROM pg_depend d
			WHERE d.objid = c.oid AND d.classid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')))
	FROM pg_sequence s JOIN pg_class c ON c.oid = s.seqrelid WHERE c.relnamespace = $1::regnamespace`,
	`SELECT format('type %s %s labels=%s', t.typname, t.typtype,
		(SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid = t.oid))
	FROM pg_type t WHERE t.typnamespace = $1::regnamespace AND t.typtype IN ('e', 'd')`,
	`SELECT format('comment %s.%s %s', c.relname, d.objsubid, d.description)
	FROM pg_description d JOIN pg_class c ON c.oid = d.objoid AND d.classoid = 'pg_class'::regclass
	WHERE c.relnamespace = $1::regnamespace`,
	`SELECT format('default privileges %s %s', d.defaclobjtype,
		(SELECT string_agg(a::text, ',' ORDER BY a::text) FROM unnest(d.defaclacl) a))
	FROM pg_default_acl d WHERE d.defaclnamespace = $1::regnamespace`,
	`SELECT format('schema acl %s', (SELECT string_agg(a::text, ',' ORDER BY a::text) FROM unnest(n.nspacl) a))
	FROM pg_namespace n WHERE n.oid = $1::regnamespace`,
}

func (c *chainSchema) snapshot(ctx context.Context) ([]string, error) {
	var lines []string
	err := pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.privileged', 'on', true)`); err != nil {
			return err
		}
		for _, query := range catalogQueries {
			found, err := textLines(ctx, tx, query, c.name)
			if err != nil {
				return fmt.Errorf("read the catalog of %s: %w", c.name, err)
			}
			lines = append(lines, found...)
		}
		tables, err := textLines(ctx, tx, `SELECT c.relname FROM pg_class c
			WHERE c.relnamespace = $1::regnamespace AND c.relkind IN ('r', 'p') AND c.relname <> 'schema_migrations'`, c.name)
		if err != nil {
			return err
		}
		for _, table := range tables {
			identifier := pgx.Identifier{table}.Sanitize()
			values, err := textLines(ctx, tx, rowValuesQuery, c.name+"."+identifier)
			if err != nil {
				return fmt.Errorf("read the columns of %s: %w", table, err)
			}
			var count int64
			var digest string
			if err := tx.QueryRow(ctx, `SELECT count(*), md5(coalesce(string_agg(r::text, E'\n' ORDER BY r::text), ''))
				FROM (SELECT ROW(`+strings.Join(values, ", ")+`) AS r FROM `+identifier+`) t`).Scan(&count, &digest); err != nil {
				return fmt.Errorf("read the rows of %s: %w", table, err)
			}
			lines = append(lines, fmt.Sprintf("rows %s count=%d md5=%s", table, count, digest))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, c.name+".", "")
	}
	sort.Strings(lines)
	return lines, nil
}

// rowValuesQuery lists what the row digest reads of each column of table $1. Ids and times a
// migration made with gen_random_uuid() or now() differ on every run, so only whether such a column is
// set counts: a uuid, date or timestamp column, and any column whose default is not a constant.
const rowValuesQuery = `SELECT CASE
		WHEN a.atttypid IN ('uuid'::regtype, 'date'::regtype, 'timestamp'::regtype, 'timestamptz'::regtype)
			OR pg_get_expr(d.adbin, d.adrelid) ~* '(random|now|timestamp|clock)'
		THEN format('%I IS NULL', a.attname)
		ELSE format('%I', a.attname)
	END
	FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
	WHERE a.attrelid = $1::regclass AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`

func textLines(ctx context.Context, tx pgx.Tx, query, schema string) ([]string, error) {
	rows, err := tx.Query(ctx, query, schema)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
