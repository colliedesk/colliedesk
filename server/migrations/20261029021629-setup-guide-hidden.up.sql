-- Each agent can hide the inbox setup guide; false shows it while setup is unfinished.
ALTER TABLE agents ADD COLUMN setup_guide_hidden boolean NOT NULL DEFAULT false;
