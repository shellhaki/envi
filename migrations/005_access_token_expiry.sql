-- Access tokens previously shared expires_at with their refresh token, so a
-- bearer token stayed valid for the whole 30-day refresh window. Give them their
-- own expiry. Live sessions keep at most 15 minutes of access validity and must
-- refresh after that, rather than inheriting the old long window.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS access_expires_at timestamptz;
UPDATE sessions SET access_expires_at=LEAST(expires_at, now()+interval '15 minutes') WHERE access_expires_at IS NULL;
ALTER TABLE sessions ALTER COLUMN access_expires_at SET NOT NULL;
CREATE INDEX IF NOT EXISTS sessions_access_lookup_idx ON sessions(access_token_hash) WHERE revoked_at IS NULL;
