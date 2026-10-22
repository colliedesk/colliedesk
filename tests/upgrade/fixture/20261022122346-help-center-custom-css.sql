-- acme's help center adds custom CSS to its theme; beta has none.
UPDATE help_centers SET custom_css = E'/* header rule */\n.masthead{border-bottom:2px solid #0b5cad}\n'
WHERE workspace_id = md5('ws-acme')::uuid;
