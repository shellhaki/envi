import { isOrgMember } from "./access";
import { all, now, one, run, uuid } from "./db";
import { conflict, forbidden } from "./errors";

export type Project = { ID: string; OrgID: string; Name: string };
export type Environment = { ID: string; ProjectID: string; Name: string; Production: boolean };

export async function list(db: D1Database, userId: string): Promise<Project[]> {
  return all<Project>(
    db,
    `SELECT DISTINCT p.id AS ID, p.org_id AS OrgID, p.name AS Name FROM projects p
     LEFT JOIN memberships m ON m.org_id=p.org_id AND m.user_id=?1
     LEFT JOIN access_grants g ON g.project_id=p.id AND g.subject_user_id=?1
     WHERE m.id IS NOT NULL OR g.id IS NOT NULL
     ORDER BY p.name`,
    userId,
  );
}

export async function create(db: D1Database, userId: string, orgId: string, name: string): Promise<Project> {
  if (!(await isOrgMember(db, userId, orgId))) throw forbidden();
  const id = uuid();
  try {
    await run(db, `INSERT INTO projects(id,org_id,name,created_at) VALUES(?,?,?,?)`, id, orgId, name, now());
  } catch {
    throw conflict("conflict", "project could not be created");
  }
  return { ID: id, OrgID: orgId, Name: name };
}

export async function listEnvironments(db: D1Database, userId: string, projectId: string): Promise<Environment[]> {
  if (!(await canSeeProject(db, userId, projectId))) throw forbidden();
  const rows = await all<{ ID: string; ProjectID: string; Name: string; is_production: number }>(
    db,
    `SELECT id AS ID, project_id AS ProjectID, name AS Name, is_production FROM environments
     WHERE project_id=? ORDER BY name`,
    projectId,
  );
  return rows.map((r) => ({ ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Production: r.is_production === 1 }));
}

export async function createEnvironment(
  db: D1Database,
  userId: string,
  projectId: string,
  name: string,
  production: boolean,
): Promise<Environment> {
  if (!(await canSeeProject(db, userId, projectId))) throw forbidden();
  const id = uuid();
  try {
    await run(
      db,
      `INSERT INTO environments(id,project_id,name,is_production,revision,created_at) VALUES(?,?,?,?,0,?)`,
      id,
      projectId,
      name,
      production ? 1 : 0,
      now(),
    );
  } catch {
    throw conflict("conflict", "environment could not be created");
  }
  return { ID: id, ProjectID: projectId, Name: name, Production: production };
}

async function canSeeProject(db: D1Database, userId: string, projectId: string): Promise<boolean> {
  const row = await one<{ ok: number }>(
    db,
    `SELECT (
       EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=?2)
       OR EXISTS(SELECT 1 FROM access_grants g WHERE g.project_id=p.id AND g.subject_user_id=?2)
     ) AS ok FROM projects p WHERE p.id=?1`,
    projectId,
    userId,
  );
  return Boolean(row?.ok);
}
