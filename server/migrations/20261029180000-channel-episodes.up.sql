-- A message being collected names the command it carries (docs/internals/channels.md "Episodes"), so a
-- cancel knows whether its sender's batch named one, and a command whether it may take in what the sender
-- said just before it. Written with the pending journal row; kept after it is processed.
ALTER TABLE channel_inbound_events ADD COLUMN command text CHECK (char_length(command) <= 40);
