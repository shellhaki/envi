// Personal API keys, which a user exchanges for a session instead of signing in through a browser.
// They live in api_tokens with user_id set (service tokens use service_identity_id instead).

import { hashToken, randomToken } from "../crypto";
import { isInvalidText, type Db } from "../db";
import { Invalid, NotFound } from "../errors";

// PREFIX makes a leaked key easy to recognise and to scan for.
export const PREFIX = "envi_";
export const DEFAULT_LIFETIME_SECONDS = 90 * 24 * 60 * 60;

export type Key = {
  id: string;
  name: string;
  permission: string;
  secret?: string; // only on create; never stored
  expires_at: Date | null;
  last_used_at: Date | null;
  created_at: Date;
};

// createKey makes a new key. A lifetime of 0 never expires.
export async function createKey(db: Db, userId: string, rawName: string, permission: string, lifetimeSeconds: number): Promise<Key> {
  const name = rawName.trim();
  if (!name) throw new Invalid("a name is required");
  if (!["read", "write", "manage"].includes(permission)) throw new Invalid(`permission must be "read", "write" or "manage"`);

  const secret = PREFIX + randomToken();
  const row = await db.one(
    `INSERT INTO api_tokens (user_id, token_hash, name, permission, expires_at)
     VALUES ($1, $2, $3, $4, CASE WHEN $5::float8 = 0 THEN NULL ELSE now() + make_interval(secs => $5::float8) END)
     RETURNING id, expires_at, created_at`,
    [userId, await hashToken(secret), name, permission, lifetimeSeconds],
  );
  return { id: row!.id, name, permission, secret, expires_at: row!.expires_at, last_used_at: null, created_at: row!.created_at };
}

// listKeys returns the user's keys, newest first. Revoked keys are left out; expired ones are kept.
export async function listKeys(db: Db, userId: string): Promise<Key[]> {
  return db.query<Key>(
    `SELECT id, coalesce(name, '') AS name, permission, expires_at, last_used_at, created_at
       FROM api_tokens
      WHERE user_id = $1 AND revoked_at IS NULL
      ORDER BY created_at DESC`,
    [userId],
  );
}

// revokeKey stops a key working and ends every session started from it.
export async function revokeKey(db: Db, userId: string, keyId: string): Promise<void> {
  let changed: number;
  try {
    changed = await db.exec(
      `UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
      [keyId, userId],
    );
  } catch (err) {
    if (isInvalidText(err)) throw new NotFound("api key not found");
    throw err;
  }
  if (changed === 0) throw new NotFound("api key not found");
  await db.exec(`UPDATE sessions SET revoked_at = now() WHERE api_key_id = $1 AND revoked_at IS NULL`, [keyId]);
}

// authenticateKey returns whose key it is and what it may do, and records that it was used.
export async function authenticateKey(db: Db, secret: string) {
  const row = await db.one(
    `SELECT user_id, permission, id FROM api_tokens
      WHERE token_hash = $1 AND user_id IS NOT NULL AND revoked_at IS NULL
        AND (expires_at IS NULL OR expires_at > now())`,
    [await hashToken(secret)],
  );
  if (!row) return undefined;
  await db.exec(`UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, [row.id]).catch(() => {});
  return { userId: row.user_id as string, permission: row.permission as string, keyId: row.id as string };
}
