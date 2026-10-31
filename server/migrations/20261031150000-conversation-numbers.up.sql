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
    ADD COLUMN display_ref text CHECK (char_length(display_ref) BETWEEN 1 AND 60),
    ADD COLUMN number_format text CHECK (char_length(number_format) BETWEEN 3 AND 40);

-- Every format a series has had: a code a customer was given stays readable after the format changes.
-- conversations.number_format is the one its code was made with.
CREATE TABLE conversation_number_formats (
    workspace_id uuid NOT NULL,
    series text NOT NULL,
    format text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, series, format),
    FOREIGN KEY (workspace_id, series) REFERENCES conversation_number_series (workspace_id, series) ON DELETE CASCADE
);

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
    LANGUAGE plpgsql IMMUTABLE ROWS 1 AS $$
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
-- share one), skipping one an import took. One made with its number (an import keeping a code) keeps it
-- and moves next past it: in a series whose code shows the year, only a number of this year or later
-- (another year's number is another code); in one whose code does not, any number (its code would be
-- the same).
CREATE FUNCTION conversations_number() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    series_format text;
    this_year integer;
BEGIN
    SELECT extract(year FROM now() AT TIME ZONE w.timezone)::integer INTO this_year FROM workspaces w WHERE w.id = NEW.workspace_id;
    IF NEW.number_year IS NULL THEN
        SELECT extract(year FROM COALESCE(NEW.created_at, now()) AT TIME ZONE w.timezone)::integer INTO NEW.number_year
            FROM workspaces w WHERE w.id = NEW.workspace_id;
    END IF;
    IF NEW.number_series = 'default' THEN
        INSERT INTO conversation_number_series (workspace_id, series, format) VALUES (NEW.workspace_id, 'default', '#{n}')
            ON CONFLICT DO NOTHING;
        INSERT INTO conversation_number_formats (workspace_id, series, format) VALUES (NEW.workspace_id, 'default', '#{n}')
            ON CONFLICT DO NOTHING;
    END IF;
    SELECT format INTO series_format FROM conversation_number_series WHERE workspace_id = NEW.workspace_id AND series = NEW.number_series FOR UPDATE;
    IF series_format IS NULL THEN
        RAISE EXCEPTION 'conversation number series % does not exist', NEW.number_series USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF NEW.number IS NULL THEN
        LOOP
            UPDATE conversation_number_series SET next = next + 1, updated_at = now()
                WHERE workspace_id = NEW.workspace_id AND series = NEW.number_series RETURNING next - 1 INTO NEW.number;
            EXIT WHEN NOT EXISTS (SELECT 1 FROM conversations c WHERE c.workspace_id = NEW.workspace_id AND c.number_series = NEW.number_series
                AND c.number = NEW.number AND (c.number_year = NEW.number_year OR strpos(series_format, '{yyyy}') = 0));
        END LOOP;
    ELSIF strpos(series_format, '{yyyy}') = 0 OR NEW.number_year >= this_year THEN
        UPDATE conversation_number_series SET next = GREATEST(next, NEW.number + 1), updated_at = now()
            WHERE workspace_id = NEW.workspace_id AND series = NEW.number_series;
    END IF;
    IF NEW.number_format IS NULL THEN
        NEW.number_format := series_format;
    END IF;
    IF NEW.display_ref IS NULL THEN
        NEW.display_ref := conversation_display_ref(NEW.number_format, NEW.number, NEW.number_year);
    END IF;
    RETURN NEW;
END
$$;

DO $$
DECLARE
    app_role text;
    name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH name IN ARRAY ARRAY['conversation_number_series', 'conversation_number_formats'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE ON %I TO %I', name, app_role);
    END LOOP;

    -- The conversations there are get their numbers next (20261031150100), 1 on in the order they were
    -- opened: the default series starts after them. ALTER TABLE above holds conversations, so none is
    -- added between this count and the trigger.
    PERFORM set_config('app.privileged', 'on', true);
    INSERT INTO conversation_number_series (workspace_id, series, format, next)
        SELECT w.id, 'default', '#{n}', (SELECT count(*) FROM conversations c WHERE c.workspace_id = w.id) + 1 FROM workspaces w;
    INSERT INTO conversation_number_formats (workspace_id, series, format) SELECT workspace_id, series, format FROM conversation_number_series;
    PERFORM set_config('app.privileged', 'off', true);
END
$$;

CREATE TRIGGER conversations_number BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION conversations_number();
