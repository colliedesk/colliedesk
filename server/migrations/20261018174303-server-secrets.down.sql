-- The only secret is the image proxy key, which the server makes again at its next start; links already
-- sent to browsers are re-signed with the next timeline read.
DROP TABLE IF EXISTS server_secrets;
