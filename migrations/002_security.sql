ALTER TABLE secrets ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='sessions' AND column_name='access_token') THEN
    ALTER TABLE sessions RENAME COLUMN access_token TO access_token_hash;
    ALTER TABLE sessions ALTER COLUMN access_token_hash TYPE bytea USING digest(access_token_hash, 'sha256');
  END IF;
END $$;

DELETE FROM sessions WHERE user_id IS NULL;
ALTER TABLE sessions ALTER COLUMN user_id SET NOT NULL;
