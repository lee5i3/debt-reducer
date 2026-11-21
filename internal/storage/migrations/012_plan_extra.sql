-- The plan's monthly payment is the minimums plus an extra amount, so the
-- extra stays the same when minimums change (a debt added, deactivated or
-- paid off). monthly_budget_cents > 0 still means a fixed total (API).
ALTER TABLE settings ADD COLUMN extra_cents INTEGER NOT NULL DEFAULT 0 CHECK (extra_cents >= 0);

-- A saved total at or above today's minimums becomes minimums + extra.
UPDATE settings
   SET extra_cents = monthly_budget_cents - (SELECT COALESCE(SUM(minimum_payment_cents), 0) FROM debts WHERE balance_cents > 0 AND inactive = 0),
       monthly_budget_cents = 0
 WHERE monthly_budget_cents > 0
   AND monthly_budget_cents >= (SELECT COALESCE(SUM(minimum_payment_cents), 0) FROM debts WHERE balance_cents > 0 AND inactive = 0);
