-- A help center other than the primary is now served at /<handle> directly below the primary's
-- root, no longer below /b/<handle>. Its handle must therefore never be a first path segment the
-- help center site routes itself (its pages, files, styles, the Freshdesk paths below /support, the
-- help chat at /chat, and b) nor shaped like a language tag the site reads as a locale segment
-- (vi, pt-br). domain.CheckHelpCenterHandle refuses the same handles; a new route is added to both.
-- No API sets a handle yet, so no stored row breaks the new rule. An older image keeps serving
-- below /b/<handle> and is not affected by the narrower set of handles.
ALTER TABLE help_centers
    ADD CONSTRAINT help_centers_handle_not_a_route
        CHECK (
            handle !~ '^[a-z]{2,3}(-[a-z0-9]{1,8})*$'
            AND handle NOT IN (
                'a', 'b', 'c', 'search', 'images', 'media', 'files', '_', 'robots.txt',
                'sitemap.xml', 'llms.txt', 'support', 'chat'
            )
        );
