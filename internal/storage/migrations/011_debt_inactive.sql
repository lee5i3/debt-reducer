-- An inactive debt stays on the Debts list but is left out of the plan and Simulate.
ALTER TABLE debts ADD COLUMN inactive INTEGER NOT NULL DEFAULT 0 CHECK (inactive IN (0, 1));
