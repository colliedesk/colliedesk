-- Rolling back would turn every merged contact into a contact of its own again with no record of what
-- the merge moved, so it refuses while any merge is in effect. Undo the merges deliberately first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM contacts WHERE merged_into_id IS NOT NULL) THEN
        RAISE EXCEPTION 'contact merge rollback refused: % contacts are merged into another',
            (SELECT count(*) FROM contacts WHERE merged_into_id IS NOT NULL);
    END IF;
END $$;

DROP TABLE IF EXISTS contact_merges;
DROP INDEX IF EXISTS contacts_merged_into_idx;
ALTER TABLE contacts DROP CONSTRAINT IF EXISTS contacts_merged_into_fkey;
ALTER TABLE contacts DROP COLUMN IF EXISTS merged_into_id;
DROP INDEX IF EXISTS conversations_workspace_contact_updated_idx;
