-- D1 port of migrations/schema.sql.
--
-- Ids stay UUID strings and timestamps are epoch milliseconds, so rows migrate
-- from Postgres without rewriting references. Token hashes stay BLOBs holding
-- the same SHA-256 bytes, and secret ciphertext keeps the Go layout
-- (nonce ‖ ciphertext ‖ tag) so both servers read the same data.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  name TEXT,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS organizations (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT 'personal' CHECK (type IN ('personal','team')),
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS memberships (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  created_at INTEGER NOT NULL,
  UNIQUE (user_id, org_id)
);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (org_id, name)
);

CREATE TABLE IF NOT EXISTS environments (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  is_production INTEGER NOT NULL DEFAULT 0,
  revision INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS secrets (
  id TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  key_name TEXT NOT NULL,
  current_version_id TEXT,
  deleted_at INTEGER,
  created_at INTEGER NOT NULL,
  UNIQUE (environment_id, key_name)
);

CREATE TABLE IF NOT EXISTS secret_versions (
  id TEXT PRIMARY KEY,
  secret_id TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  version_number INTEGER NOT NULL CHECK (version_number > 0),
  created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (secret_id, version_number)
);

CREATE TABLE IF NOT EXISTS access_grants (
  id TEXT PRIMARY KEY,
  subject_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
  permission TEXT NOT NULL CHECK (permission IN ('read','write','manage')),
  created_at INTEGER NOT NULL,
  UNIQUE (subject_user_id, project_id, environment_id, permission)
);

CREATE TABLE IF NOT EXISTS invitations (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
  org_id TEXT REFERENCES organizations(id) ON DELETE CASCADE,
  environment_id TEXT REFERENCES environments(id) ON DELETE CASCADE,
  email TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',
  permission TEXT NOT NULL DEFAULT 'read' CHECK (permission IN ('read','write','manage')),
  token_hash BLOB NOT NULL UNIQUE,
  invited_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','expired','revoked')),
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  CHECK (project_id IS NOT NULL OR org_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash BLOB NOT NULL UNIQUE,
  access_token_hash BLOB NOT NULL UNIQUE,
  access_expires_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  revoked_at INTEGER,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS service_identities (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (project_id, environment_id, name)
);

CREATE TABLE IF NOT EXISTS api_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
  service_identity_id TEXT REFERENCES service_identities(id) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE,
  permission TEXT NOT NULL CHECK (permission IN ('read','write','manage')),
  expires_at INTEGER,
  revoked_at INTEGER,
  last_used_at INTEGER,
  created_at INTEGER NOT NULL,
  CHECK ((user_id IS NULL) <> (service_identity_id IS NULL))
);

CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS device_authorizations (
  id TEXT PRIMARY KEY,
  device_code_hash BLOB NOT NULL UNIQUE,
  user_code TEXT NOT NULL UNIQUE,
  user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','redeemed')),
  expires_at INTEGER NOT NULL,
  approved_at INTEGER,
  created_at INTEGER NOT NULL
);

-- Replaces Redis: one-time codes and their attempt counters. D1 is strongly
-- consistent, which an attempt counter has to be or it can be raced past.
CREATE TABLE IF NOT EXISTS otp_codes (
  email TEXT PRIMARY KEY,
  code_hash BLOB NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rate_limits (
  bucket TEXT PRIMARY KEY,
  count INTEGER NOT NULL DEFAULT 0,
  window_start INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS environments_project_idx ON environments(project_id);
CREATE INDEX IF NOT EXISTS secrets_environment_idx ON secrets(environment_id);
CREATE INDEX IF NOT EXISTS secret_versions_secret_idx ON secret_versions(secret_id);
CREATE INDEX IF NOT EXISTS grants_project_env_idx ON access_grants(project_id, environment_id);
CREATE INDEX IF NOT EXISTS audit_org_created_idx ON audit_events(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS invitations_email_idx ON invitations(email);
CREATE INDEX IF NOT EXISTS device_authorizations_user_code_idx ON device_authorizations(user_code);
CREATE UNIQUE INDEX IF NOT EXISTS grants_project_wide_unique
  ON access_grants(subject_user_id, project_id, permission) WHERE environment_id IS NULL;
CREATE INDEX IF NOT EXISTS sessions_access_idx ON sessions(access_token_hash);
CREATE INDEX IF NOT EXISTS otp_expiry_idx ON otp_codes(expires_at);
