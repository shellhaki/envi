ALTER TABLE invitations ADD COLUMN IF NOT EXISTS environment_id uuid REFERENCES environments(id) ON DELETE CASCADE;
ALTER TABLE invitations ADD COLUMN IF NOT EXISTS permission text NOT NULL DEFAULT 'read' CHECK (permission IN ('read','write','manage'));
ALTER TABLE invitations ADD COLUMN IF NOT EXISTS token_hash bytea;
UPDATE invitations SET token_hash=digest(id::text, 'sha256') WHERE token_hash IS NULL;
ALTER TABLE invitations ALTER COLUMN token_hash SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS invitations_token_hash_idx ON invitations(token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS grants_project_wide_unique ON access_grants(subject_user_id,project_id,permission) WHERE environment_id IS NULL;
