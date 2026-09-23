-- The key-value store the SDK reads and writes.
--
-- This is deliberately not the secrets table. Secrets belong to an environment,
-- carry a version history, and are guarded by per-environment grants. These are
-- plain project-wide values an application sets and reads at runtime, with no
-- history and no environment split.
--
-- Values are encrypted with the same cipher as secrets. It costs nothing
-- noticeable, and people will put a credential in here whatever the docs say.
--
-- Safe to run while the API is serving: a new table touches nothing existing.

CREATE TABLE IF NOT EXISTS kv_entries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key_name text NOT NULL,
  ciphertext bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, key_name)
);

CREATE INDEX IF NOT EXISTS kv_entries_project_idx ON kv_entries(project_id);
