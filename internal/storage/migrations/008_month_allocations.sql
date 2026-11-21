-- An edited month can set what each account gets (its minimum, or minimum
-- plus extra) instead of one total.
CREATE TABLE plan_month_allocations (
    month        TEXT    NOT NULL,                         -- YYYY-MM-01
    debt_id      INTEGER NOT NULL,
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    PRIMARY KEY (month, debt_id)
);
