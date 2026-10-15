-- Dropping the tables removes their policies. Subscription state is rebuilt by Polar redelivering
-- events (or by re-sending subscription.updated from the Polar dashboard).
DROP TABLE IF EXISTS billing_webhook_events;
DROP TABLE IF EXISTS workspace_billing;
