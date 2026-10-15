CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Accounts --------------------------------------------------------------------

CREATE TABLE users (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email      text NOT NULL UNIQUE,
  name       text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name          text NOT NULL,
  type          text NOT NULL DEFAULT 'personal' CHECK (type IN ('personal','team')),
  -- Whether this organization wants announcements. Honoured without exception.
  is_subscribed boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role       text NOT NULL CHECK (role IN ('owner','admin','member')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, org_id)
);

-- Projects and secrets --------------------------------------------------------

CREATE TABLE projects (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);

CREATE TABLE environments (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id    uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name          text NOT NULL,
  is_production boolean NOT NULL DEFAULT false,
  revision      bigint NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);

CREATE TABLE secrets (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  environment_id     uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  key_name           text NOT NULL,
  -- The foreign key is added after secret_versions exists, below.
  current_version_id uuid,
  deleted_at         timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (environment_id, key_name)
);

CREATE TABLE secret_versions (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  secret_id      uuid NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  ciphertext     bytea NOT NULL,
  nonce          bytea NOT NULL,
  version_number integer NOT NULL CHECK (version_number > 0),
  created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (secret_id, version_number)
);
ALTER TABLE secrets ADD CONSTRAINT secrets_current_version_fk
  FOREIGN KEY (current_version_id) REFERENCES secret_versions(id) ON DELETE SET NULL;

CREATE TABLE kv_entries (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key_name   text NOT NULL,
  ciphertext bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, key_name)
);

-- Sharing ---------------------------------------------------------------------

CREATE TABLE access_grants (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id      uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  -- NULL means every non-production environment in the project.
  environment_id  uuid REFERENCES environments(id) ON DELETE CASCADE,
  permission      text NOT NULL CHECK (permission IN ('read','write','manage')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (subject_user_id, project_id, environment_id, permission)
);

CREATE TABLE invitations (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid REFERENCES projects(id) ON DELETE CASCADE,
  org_id         uuid REFERENCES organizations(id) ON DELETE CASCADE,
  environment_id uuid REFERENCES environments(id) ON DELETE CASCADE,
  email          text NOT NULL,
  role           text NOT NULL DEFAULT 'member',
  permission     text NOT NULL DEFAULT 'read' CHECK (permission IN ('read','write','manage')),
  token_hash     bytea NOT NULL UNIQUE,
  invited_by     uuid REFERENCES users(id) ON DELETE SET NULL,
  status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','expired','revoked')),
  expires_at     timestamptz NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK (project_id IS NOT NULL OR org_id IS NOT NULL)
);

-- Sign-in and tokens ----------------------------------------------------------

CREATE TABLE sessions (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash bytea NOT NULL UNIQUE,
  access_token_hash  bytea NOT NULL UNIQUE,
  access_expires_at  timestamptz NOT NULL,
  expires_at         timestamptz NOT NULL,
  revoked_at         timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  -- NULL for an ordinary login; set when the session came from an API key that
  -- caps what it may do.
  permission         text CHECK (permission IN ('read','write','manage')),
  -- The foreign key is added after api_tokens exists, below.
  api_key_id         uuid
);

CREATE TABLE service_identities (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id     uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  name           text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, environment_id, name)
);

-- Personal API keys (user_id set) and service tokens (service_identity_id set).
-- Exactly one of the two is set.
CREATE TABLE api_tokens (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             uuid REFERENCES users(id) ON DELETE CASCADE,
  service_identity_id uuid REFERENCES service_identities(id) ON DELETE CASCADE,
  token_hash          bytea NOT NULL UNIQUE,
  name                text,
  permission          text NOT NULL CHECK (permission IN ('read','write','manage')),
  expires_at          timestamptz,
  revoked_at          timestamptz,
  last_used_at        timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) <> (service_identity_id IS NULL))
);
ALTER TABLE sessions ADD CONSTRAINT sessions_api_key_id_fkey
  FOREIGN KEY (api_key_id) REFERENCES api_tokens(id) ON DELETE CASCADE;

CREATE TABLE device_authorizations (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  device_code_hash bytea NOT NULL UNIQUE,
  user_code        text NOT NULL UNIQUE,
  user_id          uuid REFERENCES users(id) ON DELETE CASCADE,
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','redeemed')),
  expires_at       timestamptz NOT NULL,
  approved_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now()
);

-- Audit -----------------------------------------------------------------------

CREATE TABLE audit_events (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_id    uuid REFERENCES users(id) ON DELETE SET NULL,
  action      text NOT NULL,
  target_type text NOT NULL,
  target_id   uuid,
  metadata    jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX environments_project_idx ON environments(project_id);
CREATE INDEX secrets_environment_idx ON secrets(environment_id);
CREATE INDEX secret_versions_secret_idx ON secret_versions(secret_id);
CREATE INDEX grants_project_env_idx ON access_grants(project_id, environment_id);
CREATE UNIQUE INDEX grants_project_wide_unique ON access_grants(subject_user_id, project_id, permission) WHERE environment_id IS NULL;
CREATE INDEX kv_entries_project_idx ON kv_entries(project_id);
CREATE INDEX api_tokens_user_idx ON api_tokens(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX sessions_api_key_idx ON sessions(api_key_id) WHERE api_key_id IS NOT NULL;
CREATE INDEX audit_org_created_idx ON audit_events(org_id, created_at DESC);
CREATE INDEX invitations_email_idx ON invitations(email);
CREATE INDEX device_authorizations_user_code_idx ON device_authorizations(user_code);

-- Operators -------------------------------------------------------------------

-- What the instance costs to run, recorded by an operator. Money is stored in
-- whole minor units (kobo, cents) and never as a float, and currency is carried
-- per row because no exchange rate is applied anywhere.
CREATE TABLE expenses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  amount_minor bigint NOT NULL CHECK (amount_minor > 0),
  currency text NOT NULL CHECK (currency IN ('NGN','USD')),
  category text NOT NULL, description text NOT NULL DEFAULT '',
  spent_on date NOT NULL DEFAULT current_date,
  recurring text NOT NULL DEFAULT 'once' CHECK (recurring IN ('once','monthly','yearly')),
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX expenses_spent_on_idx ON expenses(spent_on DESC);
CREATE INDEX expenses_recurring_idx ON expenses(recurring) WHERE recurring <> 'once';

-- Operators act on the instance as a whole. Separate from users because an
-- operator is a job, not an account. ENVI_ADMINS is the bootstrap list and
-- still wins; rows here are operators an admin added.
CREATE TABLE operators (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL UNIQUE CHECK (email = lower(email)),
  name text NOT NULL,
  role text NOT NULL CHECK (role IN ('admin','announcer')),
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- What was announced, by whom, to how many. A mass email nobody is
-- accountable for is not a feature.
CREATE TABLE announcements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject text NOT NULL, body text NOT NULL,
  sent_by uuid REFERENCES users(id) ON DELETE SET NULL,
  sent_by_email text NOT NULL DEFAULT '',
  recipients integer NOT NULL DEFAULT 0 CHECK (recipients >= 0),
  failures integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX announcements_created_idx ON announcements(created_at DESC);
CREATE INDEX organizations_subscribed_idx ON organizations(is_subscribed) WHERE is_subscribed;

-- Billing ---------------------------------------------------------------------
-- See migrations/011_billing.sql and BILLING.md.
CREATE TABLE billing_accounts (
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
CREATE TABLE billable_members (
  org_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month   date NOT NULL CHECK (month = date_trunc('month', month)::date),
  PRIMARY KEY (org_id, user_id, month)
);

-- One row per organization per month on pay as you go.
CREATE TABLE payg_charges (
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

CREATE INDEX payg_charges_retry_idx ON payg_charges(next_attempt_at) WHERE status = 'failed';

-- Every Bachs event applied, by id. Bachs delivers at least once; an id already
-- here means the event has been applied and the delivery is acknowledged
-- without doing anything.
CREATE TABLE bachs_events (
  id          text PRIMARY KEY,
  type        text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
