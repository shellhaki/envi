import type { Queryable } from "../db";
import { Forbidden } from "../errors";

// allow throws Forbidden unless the user has `need` ("read", "write" or "manage") on the environment.
// Access comes from any of:
//   - an access grant on this environment, or on the whole project for non-production environments
//   - membership of the project's organization, for non-production environments
//   - being an owner or admin of the project's organization
export async function allow(db: Queryable, userId: string, envId: string, need: string): Promise<void> {
  const row = await db
    .one(
      `SELECT
         EXISTS (
           SELECT 1 FROM access_grants g
            WHERE g.subject_user_id = $1
              AND g.project_id = e.project_id
              AND (g.environment_id = e.id OR (g.environment_id IS NULL AND NOT e.is_production))
              AND CASE $3
                    WHEN 'read'  THEN g.permission IN ('read', 'write', 'manage')
                    WHEN 'write' THEN g.permission IN ('write', 'manage')
                    ELSE g.permission = 'manage'
                  END
         )
         OR (NOT e.is_production AND EXISTS (
           SELECT 1 FROM projects p JOIN memberships m ON m.org_id = p.org_id
            WHERE p.id = e.project_id AND m.user_id = $1
         ))
         OR EXISTS (
           SELECT 1 FROM projects p JOIN memberships m ON m.org_id = p.org_id
            WHERE p.id = e.project_id AND m.user_id = $1 AND m.role IN ('owner', 'admin')
         ) AS allowed
       FROM environments e
       WHERE e.id = $2`,
      [userId || null, envId, need],
    )
    .catch(() => undefined);
  if (!row?.allowed) throw new Forbidden();
}
