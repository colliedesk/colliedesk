DROP TRIGGER messages_sla_reply ON messages;
DROP TRIGGER conversations_sla_change ON conversations;
DROP TRIGGER conversations_sla_open ON conversations;
DROP FUNCTION messages_sla_reply();
DROP FUNCTION conversations_sla_change();
DROP FUNCTION conversations_sla_open();
DROP INDEX conversations_sla_stale;
DROP INDEX conversations_sla_due;
ALTER TABLE conversations DROP COLUMN sla_warn_at, DROP COLUMN sla_due_at, DROP COLUMN sla_resolution_warn_at, DROP COLUMN sla_resolution_due_at,
    DROP COLUMN sla_resolution_state, DROP COLUMN sla_first_met_at, DROP COLUMN sla_first_warn_at, DROP COLUMN sla_first_due_at,
    DROP COLUMN sla_first_state, DROP COLUMN sla_stale, DROP COLUMN sla_applies;
