-- What running the instance costs.
--
-- Revenue is only half of whether this is a business. The other half is the VPS,
-- the domain, the email provider — numbers that live in bank statements and
-- nowhere the product can see. This table is where an operator writes them down
-- so the admin screen can put both halves on one page.
--
-- Three decisions worth stating:
--
--   amount_minor  Money is stored as a whole number of minor units — kobo for
--                 NGN, cents for USD — never as a float. 0.1 + 0.2 is not 0.3
--                 in binary floating point, and a column that quietly loses a
--                 kobo per row is worse than no column.
--
--   currency      Carried per row, with no conversion anywhere. Totals are
--                 reported per currency instead. A stored exchange rate goes
--                 stale within a day and a hardcoded one is wrong immediately;
--                 reporting "₦48,000 and $20" is honest, and "₦79,400" is a
--                 guess dressed as a fact.
--
--   recurring     A $20/month VPS is not a $20 expense. Without this, the
--                 monthly run rate cannot be worked out from the rows and the
--                 only number available is a meaningless lifetime total.
--
-- Safe to run while the API is serving: one new table, nothing else touched.

CREATE TABLE IF NOT EXISTS expenses (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  currency     text NOT NULL CHECK (currency IN ('NGN', 'USD')),
  category     text NOT NULL,
  description  text NOT NULL DEFAULT '',
  spent_on     date NOT NULL DEFAULT current_date,
  recurring    text NOT NULL DEFAULT 'once' CHECK (recurring IN ('once', 'monthly', 'yearly')),
  -- SET NULL rather than CASCADE: deleting the operator who recorded an expense
  -- must not delete the expense. The money was still spent.
  created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS expenses_spent_on_idx ON expenses(spent_on DESC);
CREATE INDEX IF NOT EXISTS expenses_recurring_idx ON expenses(recurring) WHERE recurring <> 'once';
