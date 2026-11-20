-- Every vote, line and reset goes with its tables.
ALTER TABLE help_centers DROP COLUMN votes_version;
DROP TABLE kb_article_votes;
DROP TABLE kb_article_vote_resets;
