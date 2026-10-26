-- beta: the chat message Minh collected first named the bug command. Values are fixture data.
UPDATE channel_inbound_events SET command = 'bug' WHERE id = md5('chin-beta-1')::uuid;
