-- Rolling back drops portal sign-ins and the setting. Requests already opened stay, as conversations
-- a teammate started, so none are lost. A requester signs in again after the upgrade returns.
-- The row-level security policy shows conversation rows only inside a workspace, or to a privileged
-- transaction, as the migration runner marks it.
SELECT set_config('app.privileged', 'on', true);
UPDATE conversations SET channel = 'agent' WHERE channel = 'portal';
DELETE FROM channel_types WHERE type = 'portal';
DROP TABLE portal_sessions;
DROP TABLE portal_links;
ALTER TABLE help_centers DROP COLUMN portal_enabled;
