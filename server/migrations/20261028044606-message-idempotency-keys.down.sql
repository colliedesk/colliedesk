-- The keys only stop a repeated submit from storing a message twice; the messages stay.
DROP TABLE message_idempotency_keys;
