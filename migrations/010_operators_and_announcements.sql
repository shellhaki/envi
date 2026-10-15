

ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS is_subscribed boolean NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS operators (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Lowercased on write. The lookup is by address, so two rows differing only
  -- in case would be two operators with one inbox.
  email      text NOT NULL UNIQUE CHECK (email = lower(email)),
  name       text NOT NULL,
  -- admin     — everything an operator can do, including adding operators.
  -- announcer — may write and send announcements, and nothing else.
  role       text NOT NULL CHECK (role IN ('admin', 'announcer')),
  -- SET NULL rather than CASCADE: removing the admin who appointed somebody
  -- must not silently remove that person's access too.
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS announcements (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject     text NOT NULL,
  body        text NOT NULL,
  -- Who sent it, kept as plain text as well as a reference: the record of who
  -- emailed every customer has to survive that person's row being deleted.
  sent_by     uuid REFERENCES users(id) ON DELETE SET NULL,
  sent_by_email text NOT NULL DEFAULT '',
  recipients  integer NOT NULL DEFAULT 0 CHECK (recipients >= 0),
  failures    integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS announcements_created_idx ON announcements(created_at DESC);
CREATE INDEX IF NOT EXISTS organizations_subscribed_idx
  ON organizations(is_subscribed) WHERE is_subscribed;
