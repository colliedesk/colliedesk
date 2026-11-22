-- A workspace with several help centers tells them apart in the app by name ("Hardware",
-- "Developers"). Readers never see the name: they see the title. Empty means the app shows the
-- title, or the workspace name, the way a help center without a name always looked.
ALTER TABLE help_centers
    ADD COLUMN name text NOT NULL DEFAULT ''
        CONSTRAINT help_centers_name_length CHECK (char_length(name) <= 100);
