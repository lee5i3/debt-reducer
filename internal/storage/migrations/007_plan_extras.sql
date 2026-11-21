-- One-time payments added to a plan month (kept apart from an edited
-- payment so they can be removed), and scheduled monthly amount changes
-- (a raise that starts in a later month).
ALTER TABLE plan_months ADD COLUMN extra_cents INTEGER CHECK (extra_cents >= 0);

CREATE TABLE plan_budget_changes (
    month        TEXT    PRIMARY KEY,                      -- YYYY-MM-01 the new amount starts
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0)
);
