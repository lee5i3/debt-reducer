-- A debt's balance is now its starting amount "as of" a date; what's left
-- is worked out from the plan's marked months on or after that date, and
-- marking a month no longer changes the debt.
ALTER TABLE debts ADD COLUMN start_date TEXT;
UPDATE debts SET start_date = date(created_at);

-- Months marked before this change lowered balances directly. Put each
-- debt back to its amount before the first marked month and date it then.
UPDATE debts
   SET balance_cents = (SELECT p.before_cents FROM plan_month_debts p
                         WHERE p.debt_id = debts.id ORDER BY p.month LIMIT 1),
       start_date    = (SELECT p.month FROM plan_month_debts p
                         WHERE p.debt_id = debts.id ORDER BY p.month LIMIT 1)
 WHERE id IN (SELECT debt_id FROM plan_month_debts);

UPDATE promo_items
   SET remaining_cents = (SELECT json_extract(j.value, '$.remaining')
                            FROM plan_month_debts p, json_each(p.items_before) j
                           WHERE p.items_before IS NOT NULL
                             AND json_extract(j.value, '$.id') = promo_items.id
                           ORDER BY p.month LIMIT 1)
 WHERE id IN (SELECT json_extract(j.value, '$.id')
                FROM plan_month_debts p, json_each(p.items_before) j
               WHERE p.items_before IS NOT NULL);
