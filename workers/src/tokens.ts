import { all, blob, changed, now, one, run, uuid } from "./db";
import { conflict, forbidden, notFound } from "./errors";
import { hashToken, randomToken } from "./crypto";
import type { Permission } from "./types";

export async function createServiceToken(
  db: D1Database,
  userId: string,
  projectId: string,
  envId: string,
  name: string,
  permission: Permission,
  ttlSeconds: number,
) {
  const ok = await one<{ ok: number }>(
    db,
    `SELECT (
       EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id
              WHERE p.id=?1 AND m.user_id=?2 AND m.role IN ('owner','admin'))
       AND EXISTS(SELECT 1 FROM environments WHERE id=?3 AND project_id=?1)
     ) AS ok`,
    projectId,
    userId,
    envId,
  );
  if (!ok?.ok) throw forbidden();

  const identityId = uuid();
  const token = randomToken();
  const t = now();
  try {
    await db.batch([
      db
        .prepare(`INSERT INTO service_identities(id,project_id,environment_id,name,created_at) VALUES(?,?,?,?,?)`)
        .bind(identityId, projectId, envId, name, t),
      db
        .prepare(
          `INSERT INTO api_tokens(id,service_identity_id,token_hash,permission,expires_at,created_at)
           VALUES(?,?,?,?,?,?)`,
        )
        .bind(uuid(), identityId, blob(await hashToken(token)), permission, ttlSeconds > 0 ? t + ttlSeconds * 1000 : null, t),
    ]);
  } catch {
    throw conflict("conflict", "token could not be created");
  }
  return { ID: identityId, Name: name, Permission: permission, Value: token };
}

export async function revokeServiceToken(db: D1Database, userId: string, token: string): Promise<void> {
  const count = await changed(
    db,
    `UPDATE api_tokens SET revoked_at=?1 WHERE token_hash=?2 AND revoked_at IS NULL
     AND service_identity_id IN (
       SELECT si.id FROM service_identities si JOIN projects p ON p.id=si.project_id
       JOIN memberships m ON m.org_id=p.org_id WHERE m.user_id=?3 AND m.role IN ('owner','admin'))`,
    now(),
    blob(await hashToken(token)),
    userId,
  );
  if (count === 0) throw notFound("token not found");
}

export type AuditEvent = {
  action: string;
  target_type: string;
  target_id: string;
  actor: string;
  created_at: string;
};

export async function listAuditEvents(db: D1Database, userId: string, orgId: string, limit = 50): Promise<AuditEvent[]> {
  const member = await one<{ ok: number }>(
    db,
    `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=? AND org_id=?) AS ok`,
    userId,
    orgId,
  );
  if (!member?.ok) throw forbidden();

  const rows = await all<{
    action: string;
    target_type: string;
    target_id: string | null;
    actor: string | null;
    created_at: number;
  }>(
    db,
    `SELECT a.action, a.target_type, a.target_id, u.email AS actor, a.created_at
     FROM audit_events a LEFT JOIN users u ON u.id=a.actor_id
     WHERE a.org_id=? ORDER BY a.created_at DESC LIMIT ?`,
    orgId,
    limit,
  );
  return rows.map((r) => ({
    action: r.action,
    target_type: r.target_type,
    target_id: r.target_id ?? "",
    actor: r.actor ?? "",
    created_at: new Date(r.created_at).toISOString(),
  }));
}

export async function recordAudit(
  db: D1Database,
  orgId: string,
  actorId: string | null,
  action: string,
  targetType: string,
  targetId: string,
) {
  await run(
    db,
    `INSERT INTO audit_events(id,org_id,actor_id,action,target_type,target_id,metadata,created_at)
     VALUES(?,?,?,?,?,?, '{}',?)`,
    uuid(),
    orgId,
    actorId,
    action,
    targetType,
    targetId,
    now(),
  );
}
