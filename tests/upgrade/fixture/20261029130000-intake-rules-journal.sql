-- beta's processed web form message was opened by the rules because nothing matched it.
UPDATE channel_inbound_events SET decision_basis = 'default', decision_rule = 'new_thread'
    WHERE id = (SELECT id FROM channel_inbound_events WHERE workspace_id = md5('ws-beta')::uuid AND state = 'processed' ORDER BY received_at, id LIMIT 1);
