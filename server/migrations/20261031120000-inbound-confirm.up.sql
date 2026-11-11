-- A request written over several messages, where a chat scope asks before it opens (gather
-- confirm_seconds): confirm_asked_at is when the channel asked about the messages being collected, so
-- they open once the wait is over; a message collected after the question has none, and asks again.
-- description_asked_at is when it asked for the description of a command sent alone: the batch waits for
-- one (or expires) instead of opening empty.
ALTER TABLE channel_inbound_events ADD COLUMN confirm_asked_at timestamptz, ADD COLUMN description_asked_at timestamptz;
