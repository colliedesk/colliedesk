-- beta's chat asked the customer whether their request was complete before it opened.
UPDATE channel_inbound_events SET confirm_asked_at = '2026-10-25 11:59:50+00', description_asked_at = '2026-10-25 11:59:40+00' WHERE id = md5('chin-beta-1')::uuid;
