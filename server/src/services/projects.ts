// Projects and their environments, and who may see or change them.

import type { Db } from "../db";
import { Forbidden, NotFound } from "../errors";

// Field names match what the dashboard and CLI already read.
export type Project = { ID: string; OrgID: string; Name: string };
export type Environment = { ID: string; ProjectID: string; Name: string; Production: boolean };

export const DEFAULT_ENVIRONMENT = "default";

const projectColumns = `p.id AS "ID", p.org_id AS "OrgID", p.name AS "Name"`;
const envColumns = `e.id AS "ID", e.project_id AS "ProjectID", e.name AS "Name", e.is_production AS "Production"`;

// createProject makes a project and its default environment together.
export async function createProject(db: Db, userId: string, orgId: string, name: string): Promise<Project> {
  if (!(await isOrgAdmin(db, userId, orgId))) throw new Forbidden();
  return db.transaction(async (tx) => {
    const project = await tx.one<Project>(
      `INSERT INTO projects AS p (org_id, name) VALUES ($1, $2) RETURNING ${projectColumns}`,
      [orgId, name],
    );
    await tx.exec(`INSERT INTO environments (project_id, name, is_production) VALUES ($1, $2, false)`, [project!.ID, DEFAULT_ENVIRONMENT]);
    return project!;
  });
}

// listProjects returns the projects the user is a member of or has been invited to.
export async function listProjects(db: Db, userId: string): Promise<Project[]> {
  return db.query<Project>(
    `SELECT DISTINCT ${projectColumns}
       FROM projects p
       LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $1
       LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $1
      WHERE m.id IS NOT NULL OR g.id IS NOT NULL
      ORDER BY "Name"`,
    [userId],
  );
}

// deleteProject removes the project and everything in it. Only an org owner or admin may.
export async function deleteProject(db: Db, userId: string, projectId: string): Promise<void> {
  const changed = await db.exec(
    `DELETE FROM projects p USING memberships m
      WHERE p.id = $1 AND m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')`,
    [projectId, userId],
  );
  if (changed === 0) throw new Forbidden();
}

export async function createEnvironment(db: Db, userId: string, projectId: string, name: string, production: boolean): Promise<Environment> {
  if (!(await canManage(db, userId, projectId))) throw new Forbidden();
  const env = await db.one<Environment>(
    `INSERT INTO environments AS e (project_id, name, is_production) VALUES ($1, $2, $3) RETURNING ${envColumns}`,
    [projectId, name, production],
  );
  return env!;
}

// listEnvironments returns the environments the user can see in a project.
export async function listEnvironments(db: Db, userId: string, projectId: string): Promise<Environment[]> {
  if (!(await canView(db, userId, projectId))) throw new Forbidden();
  return db.query<Environment & { created_at: Date }>(
    `SELECT DISTINCT ${envColumns}, e.created_at
       FROM environments e
       JOIN projects p ON p.id = e.project_id
       LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
       LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2
            AND (g.environment_id = e.id OR (g.environment_id IS NULL AND NOT e.is_production))
      WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
      ORDER BY e.created_at, "Name"`,
    [projectId, userId],
  ).then((rows) => rows.map(({ created_at, ...env }) => env as Environment));
}

// updateEnvironment renames an environment or changes its production flag. Only an org owner or admin may.
export async function updateEnvironment(db: Db, userId: string, envId: string, name: string, production: boolean): Promise<Environment> {
  const env = await db.one<Environment>(
    `UPDATE environments e SET name = $1, is_production = $2
       FROM projects p JOIN memberships m ON m.org_id = p.org_id
      WHERE e.id = $3 AND p.id = e.project_id AND m.user_id = $4 AND m.role IN ('owner', 'admin')
     RETURNING ${envColumns}`,
    [name, production, envId, userId],
  );
  if (!env) throw new Forbidden();
  return env;
}

// deleteEnvironment removes an environment and its secrets. Only an org owner or admin may.
export async function deleteEnvironment(db: Db, userId: string, envId: string): Promise<void> {
  const changed = await db.exec(
    `DELETE FROM environments e USING projects p, memberships m
      WHERE e.id = $1 AND p.id = e.project_id AND m.org_id = p.org_id
        AND m.user_id = $2 AND m.role IN ('owner', 'admin')`,
    [envId, userId],
  );
  if (changed === 0) throw new Forbidden();
}

// findProjectByName finds one of the user's projects by name. The error lists their projects, to spot a typo.
export async function findProjectByName(db: Db, userId: string, name: string): Promise<Project> {
  const projects = await listProjects(db, userId);
  const found = projects.find((p) => p.Name === name);
  if (found) return found;
  if (projects.length === 0) throw new NotFound(`no project named ${quote(name)}, and this account has no projects`);
  throw new NotFound(`no project named ${quote(name)}; this account can see: ${projects.map((p) => p.Name).join(", ")}`);
}

// findEnvironmentByName finds an environment in a project. The name may be empty if there is only one.
export async function findEnvironmentByName(db: Db, userId: string, projectId: string, name: string): Promise<Environment> {
  const environments = await listEnvironments(db, userId, projectId);
  if (environments.length === 0) throw new NotFound("this project has no environments yet");
  const names = environments.map((e) => e.Name).join(", ");
  const found = environments.find((e) => e.Name === name);
  if (found) return found;
  if (name === "") {
    if (environments.length === 1) return environments[0];
    throw new NotFound(`this project has several environments, so one must be named: ${names}`);
  }
  throw new NotFound(`no environment named ${quote(name)} in this project; it has: ${names}`);
}

// describeEnvironment returns an environment's name and its project's name, with no permission check.
export async function describeEnvironment(db: Db, envId: string) {
  const row = await db.one(
    `SELECT e.name AS env_name, p.name AS project_name, p.id AS project_id
       FROM environments e JOIN projects p ON p.id = e.project_id WHERE e.id = $1`,
    [envId],
  );
  if (!row) throw new NotFound("environment not found");
  return { envName: row.env_name as string, projectName: row.project_name as string, projectId: row.project_id as string };
}

// canView: a member of the project's organization, or anyone with a grant on it.
export function canView(db: Db, userId: string, projectId: string) {
  return exists(db,
    `SELECT EXISTS (
       SELECT 1 FROM projects p
         LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
         LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2
        WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
     ) AS ok`, [projectId, userId]);
}

// canWrite (project-wide data, like the key-value store): a member, or anyone with a write or manage grant.
export function canWrite(db: Db, userId: string, projectId: string) {
  return exists(db,
    `SELECT EXISTS (
       SELECT 1 FROM projects p
         LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2
         LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2 AND g.permission IN ('write', 'manage')
        WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
     ) AS ok`, [projectId, userId]);
}

// canManage: an org owner or admin, or anyone with a manage grant on the project.
export function canManage(db: Db, userId: string, projectId: string) {
  return exists(db,
    `SELECT EXISTS (
       SELECT 1 FROM projects p
         LEFT JOIN memberships m ON m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin')
         LEFT JOIN access_grants g ON g.project_id = p.id AND g.subject_user_id = $2 AND g.permission = 'manage'
        WHERE p.id = $1 AND (m.id IS NOT NULL OR g.id IS NOT NULL)
     ) AS ok`, [projectId, userId]);
}

function isOrgAdmin(db: Db, userId: string, orgId: string) {
  return exists(db,
    `SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id = $1 AND org_id = $2 AND role IN ('owner', 'admin')) AS ok`,
    [userId, orgId]);
}

// exists runs a SELECT EXISTS ... AS ok query; any error counts as false.
async function exists(db: Db, sql: string, params: unknown[]): Promise<boolean> {
  const row = await db.one(sql, params).catch(() => undefined);
  return Boolean(row?.ok);
}

// quote formats a name the way Go's %q does, for error messages.
export function quote(value: string) {
  return JSON.stringify(value);
}
