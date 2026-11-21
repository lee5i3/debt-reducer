-- What kind of debt this is (credit card, auto loan, ...). Display only.
ALTER TABLE debts ADD COLUMN type TEXT NOT NULL DEFAULT 'Other';
