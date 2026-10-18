-- The cache holds only pointers to images that the proxy fetches again on demand, so dropping it
-- loses no customer data. The stored bytes stay in attachment storage.
DROP TABLE IF EXISTS mail_image_cache;
