CREATE TABLE debts (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT    NOT NULL,
    balance_cents         INTEGER NOT NULL CHECK (balance_cents > 0),
    apr                   REAL    NOT NULL CHECK (apr >= 0),
    minimum_payment_cents INTEGER NOT NULL CHECK (minimum_payment_cents > 0),
    due_day               INTEGER NOT NULL CHECK (due_day BETWEEN 1 AND 31),
    created_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Single-row table holding plan defaults.
CREATE TABLE settings (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    monthly_budget_cents INTEGER NOT NULL DEFAULT 0 CHECK (monthly_budget_cents >= 0),
    strategy             TEXT    NOT NULL DEFAULT 'avalanche'
);

INSERT INTO settings (id) VALUES (1);
