-- Contact history and merging duplicate contacts.
--
-- A contact's history is its conversations, newest activity first, a page at a time; the index
-- serves GET /conversations?contact=.
--
-- Merging keeps both rows. The duplicate gets merged_into_id (the contact that was kept) and keeps
-- its name and email, so mail from its address is routed to the kept contact instead of creating
-- the duplicate again. contact_merges records which conversations each merge moved, so undoing it
-- moves exactly those back. A merge is undone by setting undone_at; the app role never deletes a
-- merge. Older images ignore the column and the table: they would list a merged contact as a
-- contact of its own (with no conversations) and could add mail from its address to it again.
CREATE INDEX conversations_workspace_contact_updated_idx
    ON conversations(workspace_id, contact_id, updated_at DESC, id DESC);

ALTER TABLE contacts ADD COLUMN merged_into_id uuid CHECK (merged_into_id <> id);
ALTER TABLE contacts ADD CONSTRAINT contacts_merged_into_fkey
    FOREIGN KEY (workspace_id, merged_into_id)
    REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (merged_into_id);
CREATE INDEX contacts_merged_into_idx ON contacts(workspace_id, merged_into_id)
    WHERE merged_into_id IS NOT NULL;

CREATE TABLE contact_merges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The contact that was kept, and the duplicate merged into it.
    contact_id uuid NOT NULL,
    merged_contact_id uuid NOT NULL CHECK (merged_contact_id <> contact_id),
    conversation_ids uuid[] NOT NULL DEFAULT '{}',
    merged_by_agent_id uuid,
    -- The agent token that merged, when one did.
    token_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    undone_at timestamptz,
    undone_by_agent_id uuid,
    FOREIGN KEY (workspace_id, contact_id)
        REFERENCES contacts(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, merged_contact_id)
        REFERENCES contacts(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, merged_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (merged_by_agent_id),
    FOREIGN KEY (workspace_id, undone_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (undone_by_agent_id),
    FOREIGN KEY (workspace_id, token_id)
        REFERENCES sessions(workspace_id, id) ON DELETE SET NULL (token_id)
);
CREATE INDEX contact_merges_contact_idx
    ON contact_merges(workspace_id, contact_id, created_at DESC);
-- At most one merge of a duplicate is in effect at a time.
CREATE UNIQUE INDEX contact_merges_active_idx ON contact_merges(workspace_id, merged_contact_id)
    WHERE undone_at IS NULL;

-- Same isolation as contacts. The app role reads, adds and marks merges undone, never removes one;
-- secure_workspace_table leaves every privilege in place, so the grant is narrowed in
-- 20261121132937-workspace-table-security.
SELECT secure_workspace_table('contact_merges');
