-- CSS an admin adds after the help center's theme. The server checks what it may hold before a save
-- (no url(), @import, @font-face or anything else that loads or leaves the style sheet); the size
-- check here keeps any other writer to the same bound. Empty is none.
ALTER TABLE help_centers
    ADD COLUMN custom_css text NOT NULL DEFAULT '' CHECK (octet_length(custom_css) <= 32768);
