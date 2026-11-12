// The accounts of a self-hosted instance: the users file kept in step with the database, password sign-in,
// and the projects the admin gives each contributor.

import type { Deps } from "../deps";
import { Forbidden, Invalid, NotFound, RateLimited } from "../errors";
import { canManage } from "./projects";
import type { FileUser, Role, Users } from "./instance";
import { provision } from "./workspace";

const SIGN_INS_PER_15_MINUTES = 10;

export type Grant = {
  id: string;
  project_id: string;
  project_name: string;
  environment_id: string;
  environment_name: string;
  permission: string;
};
export type Member = { id: string; email: string; role: Role; grants: Grant[] };

export function usersOf(deps: Deps): Users {
  if (!deps.users) throw new NotFound("this instance signs in by email, not with a password");
  return deps.users;
}

// syncUsers gives every user in the file an account, and signs out anyone no longer in it.
export async function syncUsers(deps: Deps): Promise<FileUser[]> {
  const users = await usersOf(deps).list();
  if (!users.some((u) => u.role === "admin")) throw new Error(`the users file ${deps.config.usersFile} has no admin`);
  for (const user of users) await provision(deps, user.email);
  const emails = users.map((u) => u.email);
  await deps.db.exec(
    `UPDATE sessions SET revoked_at = now()
      WHERE revoked_at IS NULL AND user_id IN (SELECT id FROM users WHERE NOT (lower(email) = ANY($1)))`,
    [emails],
  );
  await deps.db.exec(
    `UPDATE api_tokens SET revoked_at = now()
      WHERE revoked_at IS NULL AND user_id IN (SELECT id FROM users WHERE NOT (lower(email) = ANY($1)))`,
    [emails],
  );
  return users;
}

// signInWithPassword returns the user ID for a correct email and password.
export async function signInWithPassword(deps: Deps, rawEmail: string, password: string): Promise<string> {
  const email = rawEmail.trim().toLowerCase();
  const tries = await deps.redis.incr(`pw:${email}`, 15 * 60);
  if (tries > SIGN_INS_PER_15_MINUTES) throw new RateLimited("too many sign-in attempts; try again in 15 minutes");
  const user = await usersOf(deps).verify(email, password);
  if (!user) throw new Forbidden();
  await deps.redis.del(`pw:${email}`);
  return (await provision(deps, user.email)).userId;
}

export async function emailOf(deps: Deps, userId: string): Promise<string> {
  const row = await deps.db.one(`SELECT email FROM users WHERE id = $1`, [userId || null]).catch(() => undefined);
  if (!row) throw new NotFound("user not found");
  return row.email;
}

// roleOf is the signed-in user's role on this instance, or "" with email sign-in.
export async function roleOf(deps: Deps, userId: string): Promise<Role | ""> {
  if (!deps.users || !userId) return "";
  const user = await deps.users.find(await emailOf(deps, userId).catch(() => ""));
  return user?.role ?? "";
}

// listMembers is everyone in the users file, with the projects each one has been given.
export async function listMembers(deps: Deps): Promise<Member[]> {
  const users = await usersOf(deps).list();
  const rows = await deps.db.query(
    `SELECT u.id, lower(u.email) AS email FROM users u WHERE lower(u.email) = ANY($1)`,
    [users.map((u) => u.email)],
  );
  const ids = new Map(rows.map((r) => [r.email as string, r.id as string]));
  const grants = await deps.db.query(
    `SELECT g.id, g.subject_user_id, g.project_id, p.name AS project_name, g.permission,
            COALESCE(g.environment_id::text, '') AS environment_id, COALESCE(e.name, '') AS environment_name
       FROM access_grants g
       JOIN projects p ON p.id = g.project_id
       LEFT JOIN environments e ON e.id = g.environment_id
      WHERE g.subject_user_id = ANY($1)
      ORDER BY p.name, e.name NULLS FIRST`,
    [[...ids.values()]],
  );
  return users.map((u) => {
    const id = ids.get(u.email) ?? "";
    return {
      id,
      email: u.email,
      role: u.role,
      grants: grants.filter((g) => g.subject_user_id === id).map(({ subject_user_id, ...g }) => g as Grant),
    };
  });
}

// addMember lets a new person sign in and returns their generated password.
export async function addMember(deps: Deps, email: string) {
  const password = await usersOf(deps).add(email);
  const account = await provision(deps, email);
  return { id: account.userId, email: email.trim().toLowerCase(), password };
}

export async function resetMemberPassword(deps: Deps, userId: string) {
  const email = await emailOf(deps, userId);
  return { id: userId, email, password: await usersOf(deps).resetPassword(email) };
}

// removeMember stops someone signing in, ends their sessions and keys, and takes away their projects.
export async function removeMember(deps: Deps, userId: string) {
  const email = await emailOf(deps, userId);
  await usersOf(deps).remove(email);
  await deps.db.transaction(async (tx) => {
    await tx.exec(`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, [userId]);
    await tx.exec(`UPDATE api_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, [userId]);
    await tx.exec(`DELETE FROM access_grants WHERE subject_user_id = $1`, [userId]);
  });
}

// changeOwnPassword needs the current password, so a stolen session alone can't lock the owner out.
export async function changeOwnPassword(deps: Deps, userId: string, current: string, next: string) {
  const users = usersOf(deps);
  const email = await emailOf(deps, userId);
  if (!(await users.verify(email, current))) throw new Forbidden();
  await users.setPassword(email, next);
}

// grantAccess gives a user of this instance a project, or one environment of it, at a permission.
// It replaces whatever that user had on the same project and environment.
export async function grantAccess(
  deps: Deps, actorId: string, projectId: string, userId: string, permission: string, envId: string,
): Promise<Grant> {
  if (!["read", "write", "manage"].includes(permission)) throw new Invalid(`permission must be "read", "write" or "manage"`);
  if (!(await canManage(deps.db, actorId, projectId))) throw new Forbidden();
  const email = await emailOf(deps, userId).catch(() => "");
  if (!email || !(await usersOf(deps).find(email))) throw new Invalid("that person is not a user of this instance");

  return deps.db.transaction(async (tx) => {
    if (envId) {
      const env = await tx.one(`SELECT 1 FROM environments WHERE id::text = $1 AND project_id = $2`, [envId, projectId]);
      if (!env) throw new Invalid("that environment is not in this project");
    }
    await tx.exec(
      `DELETE FROM access_grants
        WHERE subject_user_id = $1 AND project_id = $2 AND environment_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid`,
      [userId, projectId, envId],
    );
    const row = await tx.one(
      `WITH g AS (
         INSERT INTO access_grants (subject_user_id, project_id, environment_id, permission)
         VALUES ($1, $2, NULLIF($3, '')::uuid, $4) RETURNING *
       )
       SELECT g.id, g.project_id, p.name AS project_name, g.permission,
              COALESCE(g.environment_id::text, '') AS environment_id, COALESCE(e.name, '') AS environment_name
         FROM g JOIN projects p ON p.id = g.project_id LEFT JOIN environments e ON e.id = g.environment_id`,
      [userId, projectId, envId, permission],
    );
    return row as Grant;
  });
}
