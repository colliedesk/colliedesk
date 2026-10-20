-- Snapshot and compare for tests/upgrade-test.sh and tests/restore-test.sh. psql runs it after the
-- caller has created and filled the temp table input(kind, a, b, c) and set three settings:
--   upgrade.mode       'snapshot' prints this database's snapshot;
--                      'compare' also fails when it differs from the snapshot given as input
--   upgrade.workspace  '' for the whole database, or one workspace id: then only that workspace's rows
--   upgrade.allow      '' or the input kind of the declared changes to apply ('allow')
-- Input kinds: column (table, columns), rows (table, count, md5), owner (table, workspace, count) are a
-- previous snapshot; allow (table, column) is a declared change; file (storage key, sha256, size) is
-- every file in the attachments directory now; nofiles (no columns) skips the file check.
--
-- A table's checksum is the md5 of its rows as text, sorted, over the columns of the snapshot it is
-- compared with, so a column a migration adds does not count and a dropped one fails.

SET TimeZone = 'UTC';
SET DateStyle = 'ISO, YMD';
SET IntervalStyle = 'postgres';
SET extra_float_digits = 3;
SET bytea_output = 'hex';

CREATE TEMP TABLE problems (problem text NOT NULL);
CREATE TEMP TABLE output (kind text NOT NULL, a text NOT NULL, b text NOT NULL, c text NOT NULL DEFAULT '');

DO $check$
DECLARE
    mode text := current_setting('upgrade.mode');
    only_workspace text := current_setting('upgrade.workspace');
    allow_kind text := current_setting('upgrade.allow');
    t record;
    cols text[];
    present text[];
    hashed text[];
    filter text;
    n bigint;
    sum text;
    expected record;
BEGIN
    FOR t IN
        SELECT c.relname AS name,
               array_agg(a.attname::text ORDER BY a.attnum) AS columns,
               bool_or(a.attname = 'workspace_id') AS scoped
        FROM pg_class c
        JOIN pg_namespace ns ON ns.oid = c.relnamespace AND ns.nspname = 'public'
        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
        WHERE c.relkind IN ('r', 'p') AND c.relname <> 'schema_migrations'
        GROUP BY c.relname
        ORDER BY c.relname
    LOOP
        IF only_workspace <> '' AND NOT t.scoped AND t.name <> 'workspaces' THEN
            CONTINUE;
        END IF;
        filter := CASE
            WHEN only_workspace = '' THEN 'true'
            WHEN t.name = 'workspaces' THEN format('id = %L::uuid', only_workspace)
            ELSE format('workspace_id = %L::uuid', only_workspace) END;
        IF mode = 'compare' THEN
            SELECT string_to_array(i.b, ',') INTO cols FROM input i WHERE i.kind = 'column' AND i.a = t.name;
            IF cols IS NULL THEN
                -- A table the previous release did not have.
                cols := t.columns;
            END IF;
        ELSE
            cols := t.columns;
        END IF;
        hashed := ARRAY(
            SELECT col FROM unnest(cols) AS col
            WHERE col = ANY (t.columns)
              AND NOT EXISTS (SELECT 1 FROM input i WHERE i.kind = allow_kind AND i.a = t.name AND i.b IN (col, '*')));
        FOR sum IN SELECT col FROM unnest(cols) AS col WHERE NOT (col = ANY (t.columns)) LOOP
            IF NOT EXISTS (SELECT 1 FROM input i WHERE i.kind = allow_kind AND i.a = t.name AND i.b IN (sum, '*')) THEN
                INSERT INTO problems VALUES (format('%s.%s: column is gone and tests/upgrade/allowed-changes.tsv does not declare it', t.name, sum));
            END IF;
        END LOOP;
        EXECUTE format('SELECT count(*), md5(coalesce(string_agg(r, E''\n'' ORDER BY r), '''')) FROM (SELECT ROW(%s)::text AS r FROM %I WHERE %s) s',
                       CASE WHEN cardinality(hashed) = 0 THEN 'NULL' ELSE (SELECT string_agg(quote_ident(col), ', ') FROM unnest(hashed) AS col) END,
                       t.name, filter)
            INTO n, sum;
        -- Each column on its own too, for a table whose declared change leaves the row checksum moot.
        EXECUTE format('SELECT ARRAY[%s] FROM %I WHERE %s',
                       (SELECT string_agg(format('md5(coalesce(string_agg(coalesce(%1$I::text, ''\N''), E''\n'' ORDER BY coalesce(%1$I::text, ''\N'')), ''''))', col), ', ')
                        FROM unnest(t.columns) AS col),
                       t.name, filter)
            INTO present;
        INSERT INTO output VALUES ('column', t.name, array_to_string(t.columns, ','));
        INSERT INTO output VALUES ('rows', t.name, n::text, sum);
        INSERT INTO output SELECT 'colsum', t.name || '.' || col, present[k] FROM unnest(t.columns) WITH ORDINALITY AS u(col, k);
        IF t.scoped THEN
            EXECUTE format('INSERT INTO output SELECT ''owner'', %L, workspace_id::text, count(*)::text FROM %I WHERE %s GROUP BY workspace_id',
                           t.name, t.name, filter);
        END IF;
    END LOOP;

    IF mode = 'compare' THEN
        -- Every table the snapshot had is still here with the same rows.
        FOR expected IN SELECT i.a, i.b, i.c FROM input i WHERE i.kind = 'rows' LOOP
            SELECT o.b, o.c INTO n, sum FROM output o WHERE o.kind = 'rows' AND o.a = expected.a;
            IF NOT FOUND THEN
                IF NOT EXISTS (SELECT 1 FROM input i WHERE i.kind = allow_kind AND i.a = expected.a AND i.b = '*') THEN
                    INSERT INTO problems VALUES (format('%s: table is gone (had %s rows)', expected.a, expected.b));
                END IF;
            ELSIF n <> expected.b::bigint THEN
                INSERT INTO problems VALUES (format('%s: %s rows, had %s', expected.a, n, expected.b));
            ELSIF NOT EXISTS (SELECT 1 FROM input i WHERE i.kind = allow_kind AND i.a = expected.a) THEN
                IF sum <> expected.c THEN
                    INSERT INTO problems VALUES (format('%s: same %s rows but their content changed', expected.a, n));
                END IF;
            ELSE
                -- A declared change: compare the columns it leaves alone one by one.
                INSERT INTO problems
                SELECT format('%s: same rows but the values of column %s changed', expected.a, split_part(i.a, '.', 2))
                FROM input i JOIN output o ON o.kind = 'colsum' AND o.a = i.a
                WHERE i.kind = 'colsum' AND split_part(i.a, '.', 1) = expected.a AND o.b <> i.b
                  AND NOT EXISTS (SELECT 1 FROM input x WHERE x.kind = allow_kind AND x.a = expected.a AND x.b IN (split_part(i.a, '.', 2), '*'));
            END IF;
        END LOOP;
        -- No row moved from one workspace to another.
        INSERT INTO problems
        SELECT format('%s: workspace %s has %s rows, had %s', coalesce(i.a, o.a), coalesce(i.b, o.b), coalesce(o.c, '0'), coalesce(i.c, '0'))
        FROM (SELECT * FROM input WHERE kind = 'owner') i
        FULL JOIN (SELECT * FROM output WHERE kind = 'owner') o ON o.a = i.a AND o.b = i.b
        WHERE coalesce(i.c, '0') <> coalesce(o.c, '0')
          AND EXISTS (SELECT 1 FROM input x WHERE x.kind = 'column' AND x.a = coalesce(i.a, o.a))
          AND NOT EXISTS (SELECT 1 FROM input x WHERE x.kind = allow_kind AND x.a = coalesce(i.a, o.a) AND x.b = '*');
    END IF;

    -- Every stored file a row points at is there with the bytes the row recorded.
    IF NOT EXISTS (SELECT 1 FROM input WHERE kind = 'nofiles') THEN
        INSERT INTO problems
        SELECT format('attachment %s: %s', a.id,
                      CASE WHEN f.a IS NULL THEN 'file ' || a.storage_key || ' is missing'
                           ELSE 'file ' || a.storage_key || ' has sha256 ' || f.b || ' and ' || f.c || ' bytes, the row says ' || a.sha256 || ' and ' || a.size END)
        FROM attachments a
        LEFT JOIN input f ON f.kind = 'file' AND f.a = a.storage_key
        WHERE (only_workspace = '' OR a.workspace_id = only_workspace::uuid)
          AND (f.a IS NULL OR f.b <> a.sha256 OR f.c <> a.size::text);
        IF to_regclass('mail_image_cache') IS NOT NULL THEN
            INSERT INTO problems
            SELECT format('mail image %s: file %s is %s', m.url_hash, m.storage_key, CASE WHEN f.a IS NULL THEN 'missing' ELSE f.c || ' bytes, the row says ' || m.size END)
            FROM mail_image_cache m
            LEFT JOIN input f ON f.kind = 'file' AND f.a = m.storage_key
            WHERE (only_workspace = '' OR m.workspace_id = only_workspace::uuid)
              AND (f.a IS NULL OR f.c <> m.size::text);
        END IF;
    END IF;
END
$check$;

-- Rows of one workspace never point at rows of another: for every foreign key that does not
-- carry workspace_id itself, the two rows it joins must belong to the same workspace.
DO $isolation$
DECLARE
    fk record;
    leaks bigint;
BEGIN
    FOR fk IN
        SELECT con.conname, child.relname AS child, parent.relname AS parent,
               (SELECT string_agg(format('c.%I = p.%I', ca.attname, pa.attname), ' AND ')
                FROM unnest(con.conkey, con.confkey) AS k(c, p)
                JOIN pg_attribute ca ON ca.attrelid = con.conrelid AND ca.attnum = k.c
                JOIN pg_attribute pa ON pa.attrelid = con.confrelid AND pa.attnum = k.p) AS joins
        FROM pg_constraint con
        JOIN pg_class child ON child.oid = con.conrelid
        JOIN pg_class parent ON parent.oid = con.confrelid
        JOIN pg_namespace ns ON ns.oid = child.relnamespace AND ns.nspname = 'public'
        WHERE con.contype = 'f'
          AND EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = con.conrelid AND attname = 'workspace_id' AND NOT attisdropped)
          AND EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = con.confrelid AND attname = 'workspace_id' AND NOT attisdropped)
          AND NOT EXISTS (SELECT 1 FROM unnest(con.conkey) AS k(c) JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.c
                          WHERE a.attname = 'workspace_id')
    LOOP
        EXECUTE format('SELECT count(*) FROM %I c JOIN %I p ON %s WHERE c.workspace_id <> p.workspace_id', fk.child, fk.parent, fk.joins) INTO leaks;
        IF leaks > 0 THEN
            INSERT INTO problems VALUES (format('%s: %s rows of %s point at a row of %s in another workspace', fk.conname, leaks, fk.child, fk.parent));
        END IF;
    END LOOP;
    -- Row-level security stays on, and forced, for every table that holds workspace rows.
    INSERT INTO problems
    SELECT format('%s: row-level security is %s', c.relname, CASE WHEN NOT c.relrowsecurity THEN 'off' ELSE 'not forced' END)
    FROM pg_class c
    JOIN pg_namespace ns ON ns.oid = c.relnamespace AND ns.nspname = 'public'
    WHERE c.relkind IN ('r', 'p')
      AND EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = c.oid AND attname = 'workspace_id' AND NOT attisdropped)
      AND NOT (c.relrowsecurity AND c.relforcerowsecurity);
END
$isolation$;

\pset format unaligned
\pset tuples_only on
\pset fieldsep '\t'
SELECT 'problem', problem FROM problems ORDER BY problem;
SELECT kind, a, b, c FROM output ORDER BY kind, a, b;
DO $fail$
BEGIN
    IF EXISTS (SELECT 1 FROM problems) THEN
        RAISE EXCEPTION 'upgrade check failed: % problems', (SELECT count(*) FROM problems);
    END IF;
END
$fail$;
