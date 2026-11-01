-- beta's failed reply carried a file its channel cannot carry.
UPDATE channel_outbox SET final = true WHERE workspace_id = md5('ws-beta')::uuid AND status = 'failed';
