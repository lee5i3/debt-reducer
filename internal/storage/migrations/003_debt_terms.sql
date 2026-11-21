-- Debt types are now Credit Card, Loan or Promotional Purchase; the due day
-- can be "not set" (0); loans and promotions get an end date.
-- SQLite can't change a CHECK constraint in place, so the table is rebuilt.
CREATE TABLE debts_new (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT    NOT NULL,
    type                  TEXT    NOT NULL DEFAULT 'Credit Card',
    balance_cents         INTEGER NOT NULL CHECK (balance_cents > 0),
    apr                   REAL    NOT NULL CHECK (apr >= 0),
    minimum_payment_cents INTEGER NOT NULL CHECK (minimum_payment_cents > 0),
    due_day               INTEGER NOT NULL DEFAULT 0 CHECK (due_day BETWEEN 0 AND 31),
    term_ends             TEXT,
    created_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO debts_new (id, name, type, balance_cents, apr, minimum_payment_cents, due_day, created_at, updated_at)
SELECT id, name,
       CASE WHEN type IN ('Auto loan', 'Student loan', 'Personal loan', 'Mortgage', 'Medical') THEN 'Loan'
            ELSE 'Credit Card' END,
       balance_cents, apr, minimum_payment_cents, due_day, created_at, updated_at
  FROM debts;

DROP TABLE debts;
ALTER TABLE debts_new RENAME TO debts;
