-- Share links stop working (their pages answer "turned off"); the weekly email choices are lost and
-- come back on by default with the next upgrade.
DROP TABLE results_share_links;
ALTER TABLE agents DROP COLUMN results_email;
ALTER TABLE results_state DROP COLUMN weekly_sent_week;
