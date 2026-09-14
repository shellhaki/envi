import { one } from "./db";
import { forbidden } from "./errors";
import type { Permission } from "./types";

/**
 * Permissions are ordered, so "does this grant cover what is needed" is a
 * comparison rather than a generated IN list. That keeps every placeholder
 * numbered: SQLite assigns an anonymous `?` the next free index, which silently
 * collides with explicit ones when the two are mixed.
 */
const RANK: Record<Permission, number> = { read: 1, write: 2, manage: 3 };
const RANK_SQL = `(CASE g.permission WHEN 'read' THEN 1 WHEN 'write' THEN 2 WHEN 'manage' THEN 3 ELSE 0 END)`;

/**
 * Port of internal/access. Three independent ways to hold access:
 *   - a grant covering this environment (or a project-wide grant, which never
 *     reaches production environments)
 *   - org membership, for non-production environments only
 *   - owner/admin of the org, for anything
 */
export async function allow(db: D1Database, userId: string, envId: string, need: Permission): Promise<void> {
  const row = await one<{ ok: number }>(
    db,
    `SELECT (
       EXISTS(SELECT 1 FROM access_grants g WHERE g.subject_user_id=?1 AND g.project_id=e.project_id
              AND (g.environment_id=e.id OR (g.environment_id IS NULL AND e.is_production=0))
              AND ${RANK_SQL} >= ?3)
       OR (e.is_production=0 AND EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id
              WHERE p.id=e.project_id AND m.user_id=?1))
       OR EXISTS(SELECT 1 FROM projects p JOIN memberships m ON m.org_id=p.org_id
              WHERE p.id=e.project_id AND m.user_id=?1 AND m.role IN ('owner','admin'))
     ) AS ok
     FROM environments e WHERE e.id=?2`,
    userId,
    envId,
    RANK[need],
  );
  if (!row?.ok) throw forbidden();
}

/** Can invite and manage collaborators: org owner/admin, or a manage grant. */
export async function canManage(db: D1Database, projectId: string, userId: string, envId?: string): Promise<boolean> {
  const row = await one<{ ok: number }>(
    db,
    `SELECT (
       (EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=?2 AND m.role IN ('owner','admin'))
        OR EXISTS(SELECT 1 FROM access_grants g WHERE g.project_id=p.id AND g.subject_user_id=?2 AND g.permission='manage'))
       AND (?3 IS NULL OR EXISTS(SELECT 1 FROM environments e WHERE e.id=?3 AND e.project_id=p.id))
     ) AS ok
     FROM projects p WHERE p.id=?1`,
    projectId,
    userId,
    envId || null,
  );
  return Boolean(row?.ok);
}

export async function isOrgMember(db: D1Database, userId: string, orgId: string): Promise<boolean> {
  const row = await one<{ ok: number }>(
    db,
    `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=? AND org_id=?) AS ok`,
    userId,
    orgId,
  );
  return Boolean(row?.ok);
}
