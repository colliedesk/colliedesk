-- A delivery that failed for good (channels.SendResult.Final: a file the channel cannot carry): the same
-- reply fails the same way however often it is tried, so a retry is refused and the app offers none.
ALTER TABLE channel_outbox ADD COLUMN final boolean NOT NULL DEFAULT false;
