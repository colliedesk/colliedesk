-- Old Freshdesk help center addresses name an item by its Freshdesk id alone (no account), so the
-- redirect looks the ledger up by workspace, entity and id; the primary key leads with the source.
-- Only the knowledge base rows are indexed: ticket, message and contact rows are most of the ledger
-- and never looked up this way, so their imports maintain no second index. The lookup names its
-- entity as a literal so every plan can prove this predicate (store.HelpCenterLegacyTargetSQL).
CREATE INDEX import_items_source_id_idx ON import_items(workspace_id, entity, source_id)
    WHERE entity IN ('kb_category', 'kb_folder', 'kb_article');
