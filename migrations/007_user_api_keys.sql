-- Personal API keys.
--
-- api_tokens already allowed a row to belong to a user instead of a service
-- identity (user_id, plus the CHECK that exactly one of the two is set), so no
-- new table is needed. Two columns are missing:
--
--   api_tokens.name      a service token takes its name from service_identities;
--                        a user's key has nowhere to put one.
--
--   sessions.permission  a key may be capped at read/write/manage. Exchanging it
--                        for a session has to carry that cap onto the session,
--                        or the cap would vanish the moment it was used.
--                        NULL means an ordinary, uncapped login.
--
--   sessions.api_key_id  which key a session came from, so revoking the key can
--                        end its sessions too. Without it, revoking a leaked key
--                        would leave whoever took it logged in until the session
--                        ran out on its own.
--
-- Safe to run while the API is serving: all three columns are nullable, nothing is
-- rewritten, and existing rows keep working unchanged.

ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS name text;

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS permission text
  CHECK (permission IN ('read', 'write', 'manage'));

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS api_key_id uuid REFERENCES api_tokens(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS sessions_api_key_idx ON sessions(api_key_id) WHERE api_key_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS api_tokens_user_idx ON api_tokens(user_id) WHERE user_id IS NOT NULL;
