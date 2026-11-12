// The audit log of who did what to which secret, per organization.

import type { Db } from "../db";
import { Forbidden } from "../errors";

export type AuditEvent = {
  action: string;
  target_type: string;
  target_id: string;
  actor: string; // email of the user; empty for a service token
  created_at: Date;
  metadata: unknown;
};

// listEvents returns the organization's latest 200 events. Only owners and admins may read it.
export async function listEvents(db: Db, userId: string, orgId: string): Promise<AuditEvent[]> {
  const admin = await db
    .one(
      `SELECT EXISTS (
         SELECT 1 FROM memberships WHERE user_id = $1 AND org_id = $2 AND role IN ('owner', 'admin')
       ) AS ok`,
      [userId, orgId],
    )
    .catch(() => undefined);
  if (!admin?.ok) throw new Forbidden();

  return db.query<AuditEvent>(
    `SELECT a.action, a.target_type, COALESCE(a.target_id::text, '') AS target_id,
            COALESCE(u.email, '') AS actor, a.created_at, a.metadata
       FROM audit_events a
       LEFT JOIN users u ON u.id = a.actor_id
      WHERE a.org_id = $1
      ORDER BY a.created_at DESC
      LIMIT 200`,
    [orgId],
  );
}
