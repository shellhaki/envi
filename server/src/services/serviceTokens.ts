// Service tokens are credentials bound to one environment, for CI and servers.

import { hashToken, randomToken } from "../crypto";
import type { Db } from "../db";
import { Forbidden, Invalid } from "../errors";

// Field names match what the CLI already reads.
export type Token = { ID: string; Value: string; Permission: string; ExpiresAt: Date | null };

// createToken makes a token for one environment. Only an org owner or admin may. A ttl of 0 never expires.
export async function createToken(
  db: Db, userId: string, projectId: string, envId: string, name: string, permission: string, ttlSeconds: number,
): Promise<Token> {
  if (!name) throw new Invalid("token name is required");
  if (!["read", "write", "manage"].includes(permission)) throw new Invalid("invalid permission");
  if (!(await canCreate(db, userId, projectId, envId))) throw new Forbidden();

  const value = randomToken();
  const identity = await db.one(
    `INSERT INTO service_identities (project_id, environment_id, name) VALUES ($1, $2, $3) RETURNING id`,
    [projectId, envId, name],
  );
  const token = await db.one(
    `INSERT INTO api_tokens (service_identity_id, token_hash, permission, expires_at)
     VALUES ($1, $2, $3, CASE WHEN $4::float8 = 0 THEN NULL ELSE now() + make_interval(secs => $4::float8) END)
     RETURNING expires_at`,
    [identity!.id, await hashToken(value), permission, ttlSeconds],
  );
  return { ID: identity!.id, Value: value, Permission: permission, ExpiresAt: token!.expires_at };
}

// revokeToken stops a token working. Only an owner or admin of its project's organization may.
export async function revokeToken(db: Db, userId: string, token: string): Promise<void> {
  const changed = await db
    .exec(
      `UPDATE api_tokens t SET revoked_at = now()
        WHERE t.token_hash = $1
          AND EXISTS (
            SELECT 1 FROM service_identities si
              JOIN projects p ON p.id = si.project_id
              JOIN memberships m ON m.org_id = p.org_id
             WHERE si.id = t.service_identity_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')
          )`,
      [await hashToken(token), userId || null],
    );
  if (changed === 0) throw new Forbidden();
}

// authenticateToken returns the token's identity, environment and permission.
export async function authenticateToken(db: Db, token: string) {
  const hash = await hashToken(token);
  const row = await db.one(
    `SELECT si.id, si.environment_id, t.permission
       FROM api_tokens t
       JOIN service_identities si ON si.id = t.service_identity_id
      WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())`,
    [hash],
  );
  if (!row) return undefined;
  await db.exec(`UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, [hash]).catch(() => {});
  return { serviceId: row.id as string, envId: row.environment_id as string, permission: row.permission as string };
}

async function canCreate(db: Db, userId: string, projectId: string, envId: string): Promise<boolean> {
  const row = await db
    .one(
      `SELECT EXISTS (
                SELECT 1 FROM projects p JOIN memberships m ON m.org_id = p.org_id
                 WHERE p.id = $1 AND m.user_id = $2 AND m.role IN ('owner', 'admin')
              )
          AND EXISTS (SELECT 1 FROM environments WHERE id = $3 AND project_id = $1) AS ok`,
      [projectId, userId, envId],
    )
    .catch(() => undefined);
  return Boolean(row?.ok);
}
