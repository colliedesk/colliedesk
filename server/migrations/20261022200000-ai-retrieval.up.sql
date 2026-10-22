-- What AI features may read as context (docs/internals/ai-retrieval.md): published knowledge base
-- articles and each of their translations, and resolved or closed conversations (subject, first
-- customer message, last public reply; never a private note), with the name, email and company of
-- that conversation's contact and every email address and long number masked. The text is folded
-- (accents dropped, lower case) before kb_search_vector, so "dang nhap" finds "đăng nhập".
--
-- The rows are derived: nothing here is written by a person, and every row can be built again from
-- its source. Triggers queue a source in ai_retrieval_pending when it changes; the server folds and
-- masks the text (in Go) and replaces the source's rows. A source that stops being readable (an
-- article unpublished, trashed or excluded from AI, a conversation reopened) loses its rows in the
-- same transaction, and a search checks its source again, so a queue that is behind never hands AI
-- what it should not read.

-- An admin can keep one article, in every language, away from AI. People and the help center still see it.
ALTER TABLE kb_articles ADD COLUMN ai_excluded boolean NOT NULL DEFAULT false;
ALTER TABLE kb_articles ADD COLUMN ai_excluded_at timestamptz;
ALTER TABLE kb_articles ADD COLUMN ai_excluded_by_agent_id uuid;
ALTER TABLE kb_articles ADD CONSTRAINT kb_articles_ai_excluded_at CHECK (ai_excluded = (ai_excluded_at IS NOT NULL)) NOT VALID;
ALTER TABLE kb_articles VALIDATE CONSTRAINT kb_articles_ai_excluded_at;
ALTER TABLE kb_articles ADD CONSTRAINT kb_articles_ai_excluded_by_fkey
    FOREIGN KEY (workspace_id, ai_excluded_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (ai_excluded_by_agent_id);

-- Turning AI off or on for an article is a version of it, so the history shows who did it and when.
ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN ('create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude', 'ai_include'));

-- One row per article language (kb_locale NULL is the article's own text) or per conversation.
-- No GIN index: under row-level security @@ never uses one (docs/internals/knowledge-base.md).
CREATE TABLE ai_retrieval_docs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid,
    kb_locale text,
    conversation_id uuid,
    title text NOT NULL,
    body text NOT NULL,
    folded tsvector NOT NULL,
    -- About a quarter of the characters: what the text costs in a prompt.
    tokens integer NOT NULL CHECK (tokens >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((kb_article_id IS NULL) <> (conversation_id IS NULL)),
    CHECK (kb_locale IS NULL OR kb_article_id IS NOT NULL),
    FOREIGN KEY (workspace_id, kb_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    -- MATCH SIMPLE: a row with kb_locale NULL (the article's own text) is not checked against translations.
    FOREIGN KEY (workspace_id, kb_article_id, kb_locale) REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX ai_retrieval_docs_article_idx ON ai_retrieval_docs(workspace_id, kb_article_id, kb_locale) NULLS NOT DISTINCT
    WHERE kb_article_id IS NOT NULL;
CREATE UNIQUE INDEX ai_retrieval_docs_conversation_idx ON ai_retrieval_docs(workspace_id, conversation_id) WHERE conversation_id IS NOT NULL;
-- A search reads each vector of its window where it is stored (docs/internals/ai-retrieval.md): in the
-- row, not in TOAST, where a 5 kB vector costs a lookup for every word of the query.
ALTER TABLE ai_retrieval_docs ALTER COLUMN folded SET STORAGE MAIN;
-- A search reads the newest conversations of a workspace first.
CREATE INDEX ai_retrieval_docs_recent_idx ON ai_retrieval_docs(workspace_id, updated_at DESC) WHERE conversation_id IS NOT NULL;

-- Sources to build again. Rows are only added, never updated, so a change that commits while the
-- server builds a source leaves a row of its own and is built next; the server deletes the rows it read.
-- A uuid, not a sequence: restore-workspace.sh copies a workspace's rows as they are, and copied
-- sequence values would collide with the live ones.
CREATE TABLE ai_retrieval_pending (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kb_article_id uuid,
    conversation_id uuid,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((kb_article_id IS NULL) <> (conversation_id IS NULL))
);
-- The worker finds the workspaces with rows, then reads the oldest rows of one.
CREATE INDEX ai_retrieval_pending_workspace_idx ON ai_retrieval_pending(workspace_id, created_at, id);

-- Every name and company a contact had since this release, so a conversation built again after a
-- rename still masks what its text says. Only AI retrieval reads it; it goes with the contact.
CREATE TABLE ai_retrieval_former_names (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('name', 'organization')),
    value text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, contact_id) REFERENCES contacts(workspace_id, id) ON DELETE CASCADE
);
-- Each value once, by its hash: a name of a few kilobytes would not fit in an index entry.
CREATE UNIQUE INDEX ai_retrieval_former_names_value_idx ON ai_retrieval_former_names(workspace_id, contact_id, kind, md5(value));

DO $$
DECLARE
    app_role text;
    ai_table text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH ai_table IN ARRAY ARRAY['ai_retrieval_docs', 'ai_retrieval_pending', 'ai_retrieval_former_names'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', ai_table);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', ai_table);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, ai_table, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, ai_table, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', ai_table, app_role);
    END LOOP;
END
$$;

-- An article that AI may read is queued; one it may not read loses its rows now and is queued too,
-- so a build that read it before this change commits is corrected.
CREATE FUNCTION ai_retrieval_article_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status <> 'published' OR NEW.deleted_at IS NOT NULL OR NEW.ai_excluded THEN
        DELETE FROM ai_retrieval_docs WHERE workspace_id = NEW.workspace_id AND kb_article_id = NEW.id;
    END IF;
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id) VALUES (NEW.workspace_id, NEW.id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_article_inserted AFTER INSERT ON kb_articles
    FOR EACH ROW WHEN (NEW.status = 'published') EXECUTE FUNCTION ai_retrieval_article_changed();
CREATE TRIGGER ai_retrieval_article_updated AFTER UPDATE OF title, body_md, status, deleted_at, ai_excluded ON kb_articles
    FOR EACH ROW WHEN ((OLD.title, OLD.body_md, OLD.status, OLD.deleted_at, OLD.ai_excluded) IS DISTINCT FROM (NEW.title, NEW.body_md, NEW.status, NEW.deleted_at, NEW.ai_excluded))
    EXECUTE FUNCTION ai_retrieval_article_changed();

-- A translation queues its article; a deleted one takes its row with it (the foreign key).
CREATE FUNCTION ai_retrieval_translation_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id) VALUES (NEW.workspace_id, NEW.article_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_translation_inserted AFTER INSERT ON kb_article_translations
    FOR EACH ROW EXECUTE FUNCTION ai_retrieval_translation_changed();
CREATE TRIGGER ai_retrieval_translation_updated AFTER UPDATE OF title, body_md ON kb_article_translations
    FOR EACH ROW WHEN ((OLD.title, OLD.body_md) IS DISTINCT FROM (NEW.title, NEW.body_md)) EXECUTE FUNCTION ai_retrieval_translation_changed();

-- A conversation that is resolved or closed is queued; one that is reopened loses its row now.
-- Conversations that are open, as every new one from the inbox is, cost nothing here.
CREATE FUNCTION ai_retrieval_conversation_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status NOT IN ('resolved', 'closed') THEN
        DELETE FROM ai_retrieval_docs WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.id;
    END IF;
    INSERT INTO ai_retrieval_pending(workspace_id, conversation_id) VALUES (NEW.workspace_id, NEW.id);
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_conversation_inserted AFTER INSERT ON conversations
    FOR EACH ROW WHEN (NEW.status IN ('resolved', 'closed')) EXECUTE FUNCTION ai_retrieval_conversation_changed();
CREATE TRIGGER ai_retrieval_conversation_updated AFTER UPDATE OF status, subject, contact_id ON conversations
    FOR EACH ROW WHEN ((OLD.status IN ('resolved', 'closed') OR NEW.status IN ('resolved', 'closed'))
        AND (OLD.status, OLD.subject, OLD.contact_id) IS DISTINCT FROM (NEW.status, NEW.subject, NEW.contact_id))
    EXECUTE FUNCTION ai_retrieval_conversation_changed();

-- A reply, or an imported message, on a resolved or closed conversation queues it again: an import
-- writes the conversation before its messages. Mail from the customer reopens the conversation
-- instead, so new mail from the inbox (no origin) never calls this.
CREATE FUNCTION ai_retrieval_message_added() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, conversation_id)
        SELECT c.workspace_id, c.id FROM conversations c
        WHERE c.workspace_id = NEW.workspace_id AND c.id = NEW.conversation_id AND c.status IN ('resolved', 'closed');
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_message_added AFTER INSERT ON messages
    FOR EACH ROW WHEN (NEW.kind <> 'note' AND (NEW.kind = 'outgoing' OR NEW.origin IS NOT NULL)) EXECUTE FUNCTION ai_retrieval_message_added();

-- A contact renamed, given another company, merged or unmerged: its resolved and closed conversations,
-- and those of the contact it is merged into, are built again with the new details, and the old ones
-- are kept to mask.
CREATE FUNCTION ai_retrieval_contact_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.name IS DISTINCT FROM NEW.name AND btrim(OLD.name) <> '' THEN
        INSERT INTO ai_retrieval_former_names(workspace_id, contact_id, kind, value) VALUES (OLD.workspace_id, OLD.id, 'name', OLD.name)
            ON CONFLICT DO NOTHING;
    END IF;
    IF OLD.organization IS DISTINCT FROM NEW.organization AND btrim(COALESCE(OLD.organization, '')) <> '' THEN
        INSERT INTO ai_retrieval_former_names(workspace_id, contact_id, kind, value) VALUES (OLD.workspace_id, OLD.id, 'organization', OLD.organization)
            ON CONFLICT DO NOTHING;
    END IF;
    INSERT INTO ai_retrieval_pending(workspace_id, conversation_id)
        SELECT c.workspace_id, c.id FROM conversations c
        WHERE c.workspace_id = NEW.workspace_id AND c.contact_id IN (NEW.id, NEW.merged_into_id, OLD.merged_into_id) AND c.status IN ('resolved', 'closed');
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_retrieval_contact_changed AFTER UPDATE OF name, organization, merged_into_id ON contacts
    FOR EACH ROW WHEN ((OLD.name, OLD.organization, OLD.merged_into_id) IS DISTINCT FROM (NEW.name, NEW.organization, NEW.merged_into_id))
    EXECUTE FUNCTION ai_retrieval_contact_changed();

-- What exists already is built by the server after the upgrade.
INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
    SELECT workspace_id, id FROM kb_articles WHERE status = 'published' AND deleted_at IS NULL ORDER BY workspace_id, number;
INSERT INTO ai_retrieval_pending(workspace_id, conversation_id)
    SELECT workspace_id, id FROM conversations WHERE status IN ('resolved', 'closed') ORDER BY workspace_id, updated_at DESC;
