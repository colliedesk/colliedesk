-- Conversations are numbered in series (docs/user/conversations.md "Numbers"): the workspace's default
-- series shows "#n"; a channel can open its conversations in a series of its own, shown in its own format
-- ("REF-{yyyy}-{n:04}"), which customers type back. A series only counts on (next never goes back) and
-- never restarts with the year; number_year is the year the conversation was opened in the workspace's
-- timezone, kept with the number so a code of another year (from a system that restarted every year) is
-- another conversation. start is where this server took the series over: a code below it that is not
-- here belongs to the system before.
CREATE TABLE conversation_number_series (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    series text NOT NULL CHECK (series ~ '^[a-z][a-z0-9_-]{0,19}$'),
    format text NOT NULL CHECK (char_length(format) BETWEEN 3 AND 40 AND format ~ '\{n(:0[2-8])?\}'),
    next bigint NOT NULL DEFAULT 1 CHECK (next >= 1),
    start bigint NOT NULL DEFAULT 1 CHECK (start >= 1),
    start_year integer CHECK (start_year BETWEEN 1970 AND 9999),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, series)
);

ALTER TABLE conversations ADD COLUMN number_series text NOT NULL DEFAULT 'default' CHECK (number_series ~ '^[a-z][a-z0-9_-]{0,19}$'),
    ADD COLUMN number bigint CHECK (number >= 1),
    ADD COLUMN number_year integer CHECK (number_year BETWEEN 1970 AND 9999),
    ADD COLUMN display_ref text CHECK (char_length(display_ref) BETWEEN 1 AND 60);

-- The code of number n opened in year, in format (domain.NumberFormat.Render, which tests hold to this).
CREATE FUNCTION conversation_display_ref(format text, n bigint, year integer) RETURNS text
    LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    code text := replace(replace(format, '{n}', n::text), '{yyyy}', lpad(year::text, 4, '0'));
    pad integer;
BEGIN
    FOR pad IN 2..8 LOOP
        code := replace(code, '{n:0' || pad || '}', lpad(n::text, GREATEST(pad, length(n::text)), '0'));
    END LOOP;
    RETURN code;
END
$$;

-- The number and year a person's code stands for in format, read the way they type it (another case,
-- surrounding spaces, more or fewer leading zeros); no row when it is no code of the format.
CREATE FUNCTION conversation_ref_read(format text, code text) RETURNS TABLE (n bigint, year integer)
    LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    pattern text := regexp_replace(regexp_replace(replace(format, '.', '\.'), '\{n(:0[2-8])?\}', '([0-9]{1,18})'), '\{yyyy\}', '([0-9]{4})');
    year_first boolean := strpos(format, '{yyyy}') > 0 AND strpos(format, '{yyyy}') < strpos(format, '{n');
    found text[];
BEGIN
    found := regexp_match(btrim(code), '^' || pattern || '$', 'i');
    IF found IS NULL THEN
        RETURN;
    END IF;
    IF strpos(format, '{yyyy}') = 0 THEN
        n := found[1]::bigint;
        year := NULL;
    ELSIF year_first THEN
        year := found[1]::integer;
        n := found[2]::bigint;
    ELSE
        n := found[1]::bigint;
        year := found[2]::integer;
    END IF;
    IF n >= 1 THEN
        RETURN NEXT;
    END IF;
END
$$;

-- Every conversation, however it is made, takes the next number of its series (one row lock: two never
-- share one); one made with its number (an import keeping a code) keeps it.
CREATE FUNCTION conversations_number() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    series_format text;
BEGIN
    IF NEW.number_year IS NULL THEN
        SELECT extract(year FROM COALESCE(NEW.created_at, now()) AT TIME ZONE w.timezone)::integer INTO NEW.number_year
            FROM workspaces w WHERE w.id = NEW.workspace_id;
    END IF;
    IF NEW.number_series = 'default' THEN
        INSERT INTO conversation_number_series (workspace_id, series, format) VALUES (NEW.workspace_id, 'default', '#{n}')
            ON CONFLICT DO NOTHING;
    END IF;
    IF NEW.number IS NULL THEN
        UPDATE conversation_number_series SET next = next + 1, updated_at = now()
            WHERE workspace_id = NEW.workspace_id AND series = NEW.number_series
            RETURNING next - 1, format INTO NEW.number, series_format;
    ELSE
        SELECT format INTO series_format FROM conversation_number_series WHERE workspace_id = NEW.workspace_id AND series = NEW.number_series;
    END IF;
    IF series_format IS NULL THEN
        RAISE EXCEPTION 'conversation number series % does not exist', NEW.number_series USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.display_ref IS NULL THEN
        NEW.display_ref := conversation_display_ref(series_format, NEW.number, NEW.number_year);
    END IF;
    RETURN NEW;
END
$$;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE conversation_number_series ENABLE ROW LEVEL SECURITY;
    ALTER TABLE conversation_number_series FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON conversation_number_series FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON conversation_number_series FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE ON conversation_number_series TO %I', app_role);

    -- The conversations there are: the default series, in the order they were opened.
    PERFORM set_config('app.privileged', 'on', true);
    WITH numbered AS (
        SELECT c.workspace_id, c.id, row_number() OVER (PARTITION BY c.workspace_id ORDER BY c.created_at, c.id) AS n,
            extract(year FROM c.created_at AT TIME ZONE w.timezone)::integer AS year
        FROM conversations c JOIN workspaces w ON w.id = c.workspace_id)
    UPDATE conversations c SET number = numbered.n, number_year = numbered.year, display_ref = '#' || numbered.n
        FROM numbered WHERE c.workspace_id = numbered.workspace_id AND c.id = numbered.id;
    INSERT INTO conversation_number_series (workspace_id, series, format, next)
        SELECT w.id, 'default', '#{n}', COALESCE((SELECT max(number) FROM conversations c WHERE c.workspace_id = w.id), 0) + 1 FROM workspaces w;
    PERFORM set_config('app.privileged', 'off', true);
END
$$;

ALTER TABLE conversations ALTER COLUMN number SET NOT NULL, ALTER COLUMN number_year SET NOT NULL, ALTER COLUMN display_ref SET NOT NULL,
    ADD CONSTRAINT conversations_number_key UNIQUE (workspace_id, number_series, number_year, number),
    ADD CONSTRAINT conversations_number_series_fkey FOREIGN KEY (workspace_id, number_series)
        REFERENCES conversation_number_series (workspace_id, series);
CREATE TRIGGER conversations_number BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION conversations_number();
