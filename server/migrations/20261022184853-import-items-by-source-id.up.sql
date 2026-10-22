-- Old Freshdesk help center addresses name an item by its Freshdesk id alone (no account), so the
-- redirect looks the ledger up by workspace, entity and id; the primary key leads with the source.
CREATE INDEX import_items_source_id_idx ON import_items(workspace_id, entity, source_id);
