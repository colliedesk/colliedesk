-- beta reads its Gmail inbox (its grant gained gmail.readonly at a reconnect) up to a history id; cobalt's
-- reading was refused before its grant was revoked. An upgrade must keep where each read stopped.
UPDATE mailbox_connections SET scopes = 'https://www.googleapis.com/auth/gmail.send https://www.googleapis.com/auth/gmail.readonly',
    inbox_history_id = '4815162342', inbox_read_at = '2026-11-20 12:30:00+00'
WHERE workspace_id = md5('ws-beta')::uuid;
UPDATE mailbox_connections SET inbox_error_code = 'permission', inbox_history_id = '1234', inbox_read_at = '2026-10-19 14:45:00+00'
WHERE workspace_id = md5('ws-cobalt')::uuid;
