import { Forbidden } from "../errors";
import { fail, readBody, param, type Ctx } from "../http";
import { roleOf } from "../services/members";
import * as projects from "../services/projects";

// POST /projects {org_id, name}
export async function createProject(c: Ctx) {
  const body = await readBody(c, { org_id: "string", name: "string" });
  if (!body?.org_id || !body.name) return c.json({ error: "org_id and name required" }, 400);
  if (c.env.deps.users && (await roleOf(c.env.deps, c.get("userId"))) !== "admin") {
    return c.json({ error: "on this instance, only the admin creates projects" }, 403);
  }
  try {
    return c.json(await projects.createProject(c.env.deps.db, c.get("userId"), body.org_id, body.name), 201);
  } catch (err) {
    if (err instanceof Forbidden) return c.json({ error: "forbidden" }, 403);
    return c.json({ error: "project could not be created" }, 409);
  }
}

// GET /projects
export async function listProjects(c: Ctx) {
  try {
    return c.json(await projects.listProjects(c.env.deps.db, c.get("userId")));
  } catch {
    return c.json({ error: "projects unavailable" }, 500);
  }
}

// DELETE /projects/:id removes the project and everything in it.
export async function deleteProject(c: Ctx) {
  try {
    await projects.deleteProject(c.env.deps.db, c.get("userId"), param(c, "id"));
  } catch (err) {
    if (err instanceof Forbidden) return fail(c, 403, "forbidden", "only an owner or admin can delete this project");
    return fail(c, 500, "internal", "project could not be deleted");
  }
  return c.body(null, 204);
}

// POST /projects/:id/environments {name, is_production}
export async function createEnvironment(c: Ctx) {
  const body = await readBody(c, { name: "string", is_production: "boolean" });
  if (!body?.name) return c.json({ error: "name required" }, 400);
  try {
    const env = await projects.createEnvironment(c.env.deps.db, c.get("userId"), param(c, "id"), body.name, body.is_production);
    return c.json(env, 201);
  } catch (err) {
    if (err instanceof Forbidden) return c.json({ error: "forbidden" }, 403);
    return c.json({ error: "environment could not be created" }, 409);
  }
}

// GET /projects/:id/environments
export async function listEnvironments(c: Ctx) {
  try {
    return c.json(await projects.listEnvironments(c.env.deps.db, c.get("userId"), param(c, "id")));
  } catch (err) {
    if (err instanceof Forbidden) return c.json({ error: "forbidden" }, 403);
    return c.json({ error: "environments unavailable" }, 500);
  }
}

// PATCH /environments/:id {name, is_production}
export async function updateEnvironment(c: Ctx) {
  const body = await readBody(c, { name: "string", is_production: "boolean" });
  if (!body?.name) return fail(c, 400, "invalid_request", "name required");
  try {
    const env = await projects.updateEnvironment(c.env.deps.db, c.get("userId"), param(c, "id"), body.name, body.is_production);
    return c.json(env);
  } catch (err) {
    if (err instanceof Forbidden) return fail(c, 403, "forbidden", "only an owner or admin can change this environment");
    return fail(c, 409, "conflict", "environment could not be updated");
  }
}

// DELETE /environments/:id removes the environment and its secrets.
export async function deleteEnvironment(c: Ctx) {
  try {
    await projects.deleteEnvironment(c.env.deps.db, c.get("userId"), param(c, "id"));
  } catch (err) {
    if (err instanceof Forbidden) return fail(c, 403, "forbidden", "only an owner or admin can delete this environment");
    return fail(c, 500, "internal", "environment could not be deleted");
  }
  return c.body(null, 204);
}
