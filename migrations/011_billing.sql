-- Billing: what each organization is on, who it is paying for, and what it has
-- been charged.
--
-- Three plans, all with every feature (see BILLING.md):
--   free  — the owner alone
--   payg  — $0.40 or ₦600 per extra person per month, charged to a saved card
--   team  — a flat monthly Bachs subscription, USD only
--
-- An "extra person" is somebody holding an access grant on one of the
-- organization's projects who is not a member of the organization. Accepting
-- an invitation creates a grant, not a membership, so counting memberships
-- would find nobody and bill nothing.
--
-- Safe to run while the API is serving: new tables only. Nothing reads them
-- until billing is switched on, which only happens on a hosted deployment with
-- Bachs keys configured.

-- One row per organization that has ever touched billing. An organization with
-- no row is on free.
CREATE TABLE IF NOT EXISTS billing_accounts (
  org_id               uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  plan                 text NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'payg', 'team')),
  -- The currency pay as you go is charged in. Team is always USD.
  currency             text NOT NULL DEFAULT 'USD' CHECK (currency IN ('USD', 'NGN')),
  bachs_customer_id    text UNIQUE,
  card_saved           boolean NOT NULL DEFAULT false,
  card_brand           text NOT NULL DEFAULT '',
  card_last4           text NOT NULL DEFAULT '',
  subscription_id      text UNIQUE,
  subscription_status  text NOT NULL DEFAULT '',
  cancel_at_period_end boolean NOT NULL DEFAULT false,
  current_period_end   timestamptz,
  -- The most extra people pay as you go will allow, so a runaway invitation
  -- script cannot produce a surprise bill.
  payg_cap             integer NOT NULL DEFAULT 20 CHECK (payg_cap >= 0),
  -- The newest subscription event applied. Bachs does not guarantee delivery
  -- order, so an older event arriving late must not overwrite a newer state.
  last_event_at        timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);

-- Who counted, in which month. A person counts for a month if they held access
-- at any point in it, so this is written when they are added and again by an
-- hourly sweep — removing someone before the 1st does not erase the month they
-- were there for.
CREATE TABLE IF NOT EXISTS billable_members (
  org_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month   date NOT NULL CHECK (month = date_trunc('month', month)::date),
  PRIMARY KEY (org_id, user_id, month)
);

-- One row per organization per month on pay as you go.
CREATE TABLE IF NOT EXISTS payg_charges (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  month           date NOT NULL CHECK (month = date_trunc('month', month)::date),
  currency        text NOT NULL CHECK (currency IN ('USD', 'NGN')),
  members         integer NOT NULL CHECK (members >= 0),
  -- This month's own amount, in minor units.
  amount_minor    bigint NOT NULL CHECK (amount_minor >= 0),
  -- Earlier months too small to charge on their own, added to this one.
  carried_minor   bigint NOT NULL DEFAULT 0 CHECK (carried_minor >= 0),
  --   carried    — below the minimum; waiting to be added to a later month
  --   processing — sent to Bachs, waiting for its webhook
  --   succeeded  — paid
  --   failed     — refused; retried until attempts run out
  --   settled    — a carried month paid as part of a later charge
  status          text NOT NULL CHECK (status IN ('carried', 'processing', 'succeeded', 'failed', 'settled')),
  settled_by      uuid REFERENCES payg_charges(id) ON DELETE SET NULL,
  bachs_charge_id text,
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  last_error      text NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, month)
);

CREATE INDEX IF NOT EXISTS payg_charges_retry_idx ON payg_charges(next_attempt_at) WHERE status = 'failed';

-- Every Bachs event applied, by id. Bachs delivers at least once; an id already
-- here means the event has been applied and the delivery is acknowledged
-- without doing anything.
CREATE TABLE IF NOT EXISTS bachs_events (
  id          text PRIMARY KEY,
  type        text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
