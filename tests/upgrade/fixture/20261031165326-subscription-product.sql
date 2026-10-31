-- The two subscriptions of the billing fixture, now with the Polar product each runs on.
UPDATE workspace_billing SET polar_product_id = 'prod_fixture_founding' WHERE polar_subscription_id = 'sub_fixture_acme';
UPDATE workspace_billing SET polar_product_id = 'prod_fixture_team_monthly' WHERE polar_subscription_id = 'sub_fixture_cobalt';
