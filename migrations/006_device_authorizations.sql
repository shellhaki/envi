-- Device authorization grant (RFC 8628): the CLI starts a pending authorization
-- and polls it while the user approves the short user_code from an already
-- authenticated web session. device_code_hash is the secret the CLI polls with;
-- user_code is the short human-typed code. user_id is bound at approval.
CREATE TABLE IF NOT EXISTS device_authorizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  device_code_hash bytea NOT NULL UNIQUE,
  user_code text NOT NULL UNIQUE,
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','redeemed')),
  expires_at timestamptz NOT NULL,
  approved_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_authorizations_user_code_idx ON device_authorizations(user_code);
