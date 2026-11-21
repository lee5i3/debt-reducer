-- The plan is saved and tracked month by month. Marking a month paid lowers
-- debt balances, so a paid-off debt keeps a zero balance (the CHECK changes,
-- which SQLite can only do by rebuilding the table).
CREATE TABLE debts_new (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT    NOT NULL,
    type                  TEXT    NOT NULL DEFAULT 'Credit Card',
    balance_cents         INTEGER NOT NULL CHECK (balance_cents >= 0),
    apr                   REAL    NOT NULL CHECK (apr >= 0),
    minimum_payment_cents INTEGER NOT NULL CHECK (minimum_payment_cents > 0),
    due_day               INTEGER NOT NULL DEFAULT 0 CHECK (due_day BETWEEN 0 AND 31),
    term_ends             TEXT,
    created_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO debts_new (id, name, type, balance_cents, apr, minimum_payment_cents, due_day, term_ends, created_at, updated_at)
SELECT id, name, type, balance_cents, apr, minimum_payment_cents, due_day, term_ends, created_at, updated_at
  FROM debts;

DROP TABLE debts;
ALTER TABLE debts_new RENAME TO debts;

-- First month of the saved plan (YYYY-MM-01) and what was owed when it started.
ALTER TABLE settings ADD COLUMN plan_start TEXT;
ALTER TABLE settings ADD COLUMN start_total_cents INTEGER NOT NULL DEFAULT 0;

-- One row per plan month that has been marked or had its payment edited.
CREATE TABLE plan_months (
    month          TEXT    PRIMARY KEY,                -- YYYY-MM-01
    status         TEXT    NOT NULL DEFAULT '' CHECK (status IN ('', 'paid', 'skip')),
    override_cents INTEGER CHECK (override_cents >= 0), -- edited payment for the month
    paid_cents     INTEGER NOT NULL DEFAULT 0,
    interest_cents INTEGER NOT NULL DEFAULT 0,
    marked_at      TEXT
);

-- What each debt got in a marked month, so history shows exactly what happened
-- and Undo can restore balances.
CREATE TABLE plan_month_debts (
    month          TEXT    NOT NULL REFERENCES plan_months (month) ON DELETE CASCADE,
    debt_id        INTEGER NOT NULL,
    debt_name      TEXT    NOT NULL,
    before_cents   INTEGER NOT NULL,
    interest_cents INTEGER NOT NULL,
    paid_cents     INTEGER NOT NULL,
    left_cents     INTEGER NOT NULL,
    role           TEXT    NOT NULL,
    PRIMARY KEY (month, debt_id)
);
