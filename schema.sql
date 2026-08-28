CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE,
  name text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE organizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL,
  type text NOT NULL DEFAULT 'personal' CHECK (type IN ('personal','team')),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE memberships (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role text NOT NULL CHECK (role IN ('owner','admin','member')), created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, org_id)
);
CREATE TABLE projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE (org_id, name)
);
CREATE TABLE environments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name text NOT NULL, is_production boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);
CREATE TABLE secrets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  key_name text NOT NULL, current_version_id uuid, deleted_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (environment_id, key_name)
);
CREATE TABLE secret_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), secret_id uuid NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  ciphertext bytea NOT NULL, nonce bytea NOT NULL, version_number integer NOT NULL CHECK (version_number > 0),
  created_by uuid REFERENCES users(id) ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (secret_id, version_number)
);
ALTER TABLE secrets ADD CONSTRAINT secrets_current_version_fk FOREIGN KEY (current_version_id) REFERENCES secret_versions(id) ON DELETE SET NULL;
CREATE TABLE access_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), subject_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE, environment_id uuid REFERENCES environments(id) ON DELETE CASCADE,
  permission text NOT NULL CHECK (permission IN ('read','write','manage')), created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (subject_user_id, project_id, environment_id, permission)
);
CREATE TABLE invitations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
  org_id uuid REFERENCES organizations(id) ON DELETE CASCADE, environment_id uuid REFERENCES environments(id) ON DELETE CASCADE,
  email text NOT NULL, role text NOT NULL DEFAULT 'member', permission text NOT NULL DEFAULT 'read' CHECK (permission IN ('read','write','manage')),
  token_hash bytea NOT NULL UNIQUE,
  invited_by uuid REFERENCES users(id) ON DELETE SET NULL, status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','expired','revoked')),
  expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), CHECK (project_id IS NOT NULL OR org_id IS NOT NULL)
);
CREATE TABLE sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash bytea NOT NULL UNIQUE, access_token_hash bytea NOT NULL UNIQUE, expires_at timestamptz NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE service_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE, name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, environment_id, name)
);
CREATE TABLE api_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  service_identity_id uuid REFERENCES service_identities(id) ON DELETE CASCADE, token_hash bytea NOT NULL UNIQUE,
  permission text NOT NULL CHECK (permission IN ('read','write','manage')), expires_at timestamptz, revoked_at timestamptz,
  last_used_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), CHECK ((user_id IS NULL) <> (service_identity_id IS NULL))
);
CREATE TABLE audit_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL, action text NOT NULL, target_type text NOT NULL,
  target_id uuid, metadata jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX environments_project_idx ON environments(project_id);
CREATE INDEX secrets_environment_idx ON secrets(environment_id);
CREATE INDEX secret_versions_secret_idx ON secret_versions(secret_id);
CREATE INDEX grants_project_env_idx ON access_grants(project_id, environment_id);
CREATE INDEX audit_org_created_idx ON audit_events(org_id, created_at DESC);
CREATE INDEX invitations_email_idx ON invitations(email);
CREATE UNIQUE INDEX grants_project_wide_unique ON access_grants(subject_user_id,project_id,permission) WHERE environment_id IS NULL;
