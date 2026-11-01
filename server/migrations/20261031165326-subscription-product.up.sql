-- The Polar product a subscription runs on. The product says which price and period the workspace pays
-- (monthly, yearly, Founding's every 3 months), which nothing else stored tells. A row written before
-- this column has none and is filled by the next webhook or `collie billing reconcile`; it reads as ''.
-- Older images ignore the column.
ALTER TABLE workspace_billing ADD COLUMN polar_product_id text NOT NULL DEFAULT '';
