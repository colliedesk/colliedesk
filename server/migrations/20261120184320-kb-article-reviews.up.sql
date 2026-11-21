-- Review and approval before publishing (docs/internals/knowledge-base.md, "Review and approval").
-- A workspace, or one collection, can require that an article or a translation is approved by a
-- teammate before it goes on the help center. The author sends the unpublished text for review to
-- one or more people who may publish; one of them approves it or asks for changes. An approval
-- holds for the exact text it was given: content_hash covers the title, slug, body, collections and
-- SEO fields, so any edit after it makes the text unapproved again.
--
-- help_centers.require_approval is the workspace setting, off by default.
-- kb_collections.require_approval is inherit (the workspace setting), on or off; an article needs
-- approval when any of its collections does. No existing row changes: every workspace and
-- collection keeps publishing as before.
ALTER TABLE help_centers ADD COLUMN require_approval boolean NOT NULL DEFAULT false;
ALTER TABLE kb_collections ADD COLUMN require_approval text NOT NULL DEFAULT 'inherit'
    CHECK (require_approval IN ('inherit', 'on', 'off'));

-- One review of one text. locale NULL is the article's own text; otherwise the translation in that
-- language. An edit while a review is open supersedes it and opens the next one on the new text
-- (supersedes_review_id). A request sent through an agent token records it: that token, and every
-- other token of the person who made it, can never approve it.
CREATE TABLE kb_reviews (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    article_id uuid NOT NULL,
    locale text CHECK (char_length(locale) BETWEEN 2 AND 35),
    content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    state text NOT NULL DEFAULT 'open'
        CHECK (state IN ('open', 'approved', 'changes_requested', 'withdrawn', 'superseded')),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500 AND strpos(note, E'\n') = 0),
    requested_by_agent_id uuid,
    requested_by_token_id uuid,
    requested_by_token_label text,
    decided_by_agent_id uuid,
    decided_by_token_id uuid,
    decided_by_token_label text,
    decided_at timestamptz,
    decision_note text NOT NULL DEFAULT ''
        CHECK (char_length(decision_note) <= 500 AND strpos(decision_note, E'\n') = 0),
    supersedes_review_id uuid,
    -- An approved review whose text is still the one waiting to be published: the Approved list.
    -- An edit, a publish or a discard sets it false; an edit back to the approved text sets it
    -- true again.
    approves_pending boolean NOT NULL DEFAULT false,
    -- The email that tells the author the review was decided.
    author_email text NOT NULL DEFAULT 'none'
        CHECK (author_email IN ('none', 'queued', 'sending', 'sent', 'rejected', 'off')),
    author_email_claimed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    CHECK ((state = 'open') = (decided_at IS NULL)),
    FOREIGN KEY (workspace_id, article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, article_id, locale)
        REFERENCES kb_article_translations(workspace_id, article_id, locale) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, requested_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by_agent_id),
    FOREIGN KEY (workspace_id, requested_by_token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (requested_by_token_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    FOREIGN KEY (workspace_id, decided_by_token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (decided_by_token_id),
    FOREIGN KEY (workspace_id, supersedes_review_id)
        REFERENCES kb_reviews(workspace_id, id) ON DELETE SET NULL (supersedes_review_id)
);
CREATE UNIQUE INDEX kb_reviews_open_idx
    ON kb_reviews(workspace_id, article_id, COALESCE(locale, ''))
    WHERE state = 'open';
CREATE INDEX kb_reviews_article_idx ON kb_reviews(workspace_id, article_id, created_at DESC);
CREATE INDEX kb_reviews_approved_idx ON kb_reviews(workspace_id, article_id) WHERE approves_pending;
CREATE INDEX kb_reviews_author_email_idx ON kb_reviews(created_at)
    WHERE author_email IN ('queued', 'sending');

-- The people asked to review. Each gets an email (email) and sees the review in their Reviews list.
CREATE TABLE kb_review_reviewers (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    review_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    email text NOT NULL DEFAULT 'queued'
        CHECK (email IN ('queued', 'sending', 'sent', 'rejected', 'off')),
    email_claimed_at timestamptz,
    notified_at timestamptz,
    seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, review_id, agent_id),
    FOREIGN KEY (workspace_id, review_id)
        REFERENCES kb_reviews(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_review_reviewers_agent_idx ON kb_review_reviewers(workspace_id, agent_id);
CREATE INDEX kb_review_reviewers_email_idx ON kb_review_reviewers(created_at)
    WHERE email IN ('queued', 'sending');

-- A request, an approval, a request for changes and a withdrawal are versions of the article or
-- translation, so its history shows who sent which text for review and who approved it.
ALTER TABLE kb_article_versions DROP CONSTRAINT kb_article_versions_kind_check;
ALTER TABLE kb_article_versions ADD CONSTRAINT kb_article_versions_kind_check
    CHECK (kind IN (
        'create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude',
        'ai_include', 'draft', 'review_request', 'review_approve', 'review_changes',
        'review_withdraw'
    ));
ALTER TABLE kb_article_translation_versions
    DROP CONSTRAINT kb_article_translation_versions_kind_check;
ALTER TABLE kb_article_translation_versions
    ADD CONSTRAINT kb_article_translation_versions_kind_check
    CHECK (kind IN (
        'create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace',
        'mark_current', 'mark_outdated', 'draft', 'review_request', 'review_approve',
        'review_changes', 'review_withdraw'
    ));

SELECT secure_workspace_table('kb_reviews');
SELECT secure_workspace_table('kb_review_reviewers');
