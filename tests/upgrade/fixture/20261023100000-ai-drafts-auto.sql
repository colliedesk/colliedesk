-- acme lets drafts write themselves, at most 80 a day; its third draft was written for the customer's
-- first message, which the model thought needed a reply. beta keeps automatic drafts off; its draft
-- answered the customer's message too, and needed no reply.
UPDATE ai_slots SET automatic = true WHERE workspace_id = md5('ws-acme')::uuid AND slot = 'draft';
UPDATE ai_automatic_limits SET draft_per_day = 80 WHERE workspace_id = md5('ws-acme')::uuid;
UPDATE ai_drafts SET automatic = true, answers_message_id = md5('msg-acme-1-1')::uuid, reply_needed = true WHERE id = md5('aid-acme-1')::uuid;
UPDATE ai_drafts SET automatic = true, answers_message_id = md5('msg-beta-1-in')::uuid, reply_needed = false WHERE id = md5('aid-beta-1')::uuid;
