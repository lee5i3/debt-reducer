-- A "custom" plan type orders debts by interest rate or balance, highest or
-- lowest first.
ALTER TABLE settings ADD COLUMN order_by TEXT NOT NULL DEFAULT 'interest';
ALTER TABLE settings ADD COLUMN order_first TEXT NOT NULL DEFAULT 'high';
