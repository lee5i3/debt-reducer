-- A promotional purchase account holds one or more purchases, each with its
-- own promotion expiration. The debt's balance is the sum of what remains.
CREATE TABLE promo_items (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    debt_id         INTEGER NOT NULL REFERENCES debts (id) ON DELETE CASCADE,
    remaining_cents INTEGER NOT NULL CHECK (remaining_cents >= 0),
    ends            TEXT    NOT NULL                                -- YYYY-MM-DD the promotion expires
);
CREATE INDEX promo_items_debt ON promo_items (debt_id);

-- Existing promotions become a single purchase.
INSERT INTO promo_items (debt_id, remaining_cents, ends)
SELECT id, balance_cents, term_ends
  FROM debts
 WHERE type = 'Promotional Purchase' AND term_ends IS NOT NULL;

-- Marking a plan month lowers promo purchases too; keep what they were so
-- Undo can put them back (JSON list of {"id","remaining"}).
ALTER TABLE plan_month_debts ADD COLUMN items_before TEXT;
