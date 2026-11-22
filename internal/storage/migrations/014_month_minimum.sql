-- Keep each marked month's minimum with its row, so editing a debt's minimum
-- later doesn't change how past payments split into minimum and extra.
ALTER TABLE plan_month_debts ADD COLUMN minimum_cents INTEGER NOT NULL DEFAULT 0 CHECK (minimum_cents >= 0);

UPDATE plan_month_debts
   SET minimum_cents = MIN(paid_cents, COALESCE((SELECT d.minimum_payment_cents FROM debts d WHERE d.id = plan_month_debts.debt_id), paid_cents));
