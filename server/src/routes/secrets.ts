// Every secrets route works for a signed-in person or for a service token.
// A service token may only touch its own environment, and a read token may not write.

import { Conflict, Forbidden, Invalid } from "../errors";
import { fail, forbidden, readBody, readStringMap, param, type Ctx } from "../http";
import * as kv from "../services/kv";
import * as projects from "../services/projects";
import * as secrets from "../services/secrets";

const isServiceToken = (c: Ctx) => Boolean(c.get("serviceId"));
const tokenMayRead = (c: Ctx) => c.get("serviceEnv") === param(c, "id");
const tokenMayWrite = (c: Ctx) => tokenMayRead(c) && c.get("servicePermission") !== "read";

// GET /environments/:id/secrets
export async function getSecrets(c: Ctx) {
  const { db, cipher } = c.env.deps;
  const envId = param(c, "id");
  try {
    if (isServiceToken(c)) {
      if (!tokenMayRead(c)) return forbidden(c);
      return c.json(await secrets.readForService(db, cipher, c.get("serviceId"), envId));
    }
    return c.json((await secrets.snapshot(db, cipher, c.get("userId"), envId)).values);
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "secrets unavailable");
  }
}

// GET /environments/:id/secrets/snapshot returns every value plus the environment's revision.
export async function getSnapshot(c: Ctx) {
  const { db, cipher } = c.env.deps;
  const envId = param(c, "id");
  try {
    if (isServiceToken(c)) {
      if (!tokenMayRead(c)) return forbidden(c);
      const values = await secrets.readForService(db, cipher, c.get("serviceId"), envId);
      return c.json({ values, revision: await secrets.revision(db, envId) });
    }
    return c.json(await secrets.snapshot(db, cipher, c.get("userId"), envId));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "secrets unavailable");
  }
}

// PUT /environments/:id/secrets {KEY: value, ...} sets the given keys and leaves the rest.
export async function putSecrets(c: Ctx) {
  const values = await readStringMap(c);
  if (!values) return fail(c, 400, "invalid_request", "secret map required");
  if (isServiceToken(c) && !tokenMayWrite(c)) return forbidden(c);
  const { db, cipher } = c.env.deps;
  try {
    for (const [key, value] of Object.entries(values)) {
      await secrets.put(db, cipher, isServiceToken(c) ? "" : c.get("userId"), param(c, "id"), key, value);
    }
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "secret write failed");
  }
  return c.body(null, 204);
}

// PUT /environments/:id/secrets/snapshot {values, expected_revision} replaces the whole environment.
// It answers 409 when someone else wrote since expected_revision.
export async function putSnapshot(c: Ctx) {
  const body = await readBody(c, { values: "strings", expected_revision: "number" });
  if (!body?.values) return fail(c, 400, "invalid_request", "secret snapshot required");
  if (isServiceToken(c) && !tokenMayWrite(c)) return forbidden(c);
  const { db, cipher } = c.env.deps;
  try {
    const userId = isServiceToken(c) ? "" : c.get("userId");
    const revision = await secrets.putAll(db, cipher, userId, param(c, "id"), body.values, body.expected_revision);
    return c.json({ revision });
  } catch (err) {
    if (err instanceof Conflict) return fail(c, 409, "stale_revision", "remote secrets changed; run envi diff or envi pull");
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "secret write failed");
  }
}

// DELETE /environments/:id/secrets/:key
export async function deleteSecret(c: Ctx) {
  try {
    await secrets.remove(c.env.deps.db, c.get("userId"), param(c, "id"), param(c, "key"));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 404, "not_found", "secret not found");
  }
  return c.body(null, 204);
}

// GET /values?project=&environment= reads an environment by name instead of by ID.
// A service token always gets its own environment, and is refused if the names say otherwise.
export async function getValues(c: Ctx) {
  const { db, cipher } = c.env.deps;
  const wantProject = c.req.query("project") ?? "";
  const wantEnvironment = c.req.query("environment") ?? "";

  if (isServiceToken(c)) {
    const envId = c.get("serviceEnv");
    let names;
    try {
      names = await projects.describeEnvironment(db, envId);
    } catch {
      return fail(c, 500, "internal", "environment unavailable");
    }
    if ((wantProject && wantProject !== names.projectName) || (wantEnvironment && wantEnvironment !== names.envName)) {
      return fail(c, 403, "forbidden", `this token reads ${names.projectName}/${names.envName} and nothing else`);
    }
    try {
      const values = await secrets.readForService(db, cipher, c.get("serviceId"), envId);
      return c.json(valuesReply(names.projectName, names.envName, values, await secrets.revision(db, envId)));
    } catch {
      return fail(c, 500, "internal", "secrets unavailable");
    }
  }

  const userId = c.get("userId");
  if (!wantProject) return fail(c, 400, "invalid_request", "a project is required");
  let project, env;
  try {
    project = await projects.findProjectByName(db, userId, wantProject);
  } catch (err) {
    return fail(c, 404, "project_not_found", (err as Error).message);
  }
  try {
    env = await projects.findEnvironmentByName(db, userId, project.ID, wantEnvironment);
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 404, "environment_not_found", (err as Error).message);
  }
  try {
    const snapshot = await secrets.snapshot(db, cipher, userId, env.ID);
    return c.json(valuesReply(project.Name, env.Name, snapshot.values, snapshot.revision));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "secrets unavailable");
  }
}

function valuesReply(project: string, environment: string, values: Record<string, string>, revision: number) {
  return { project, environment, values, revision };
}

// --- key-value store -----------------------------------------------------------

type KvTarget = { projectId: string; projectName: string; canWrite: boolean };

// kvTarget finds the project for a key-value request. A service token acts on its environment's
// project; a person names the project. On failure it returns the reply to send.
async function kvTarget(c: Ctx, wantProject: string): Promise<KvTarget | Response> {
  const { db } = c.env.deps;
  if (isServiceToken(c)) {
    let names;
    try {
      names = await projects.describeEnvironment(db, c.get("serviceEnv"));
    } catch {
      return fail(c, 500, "internal", "project unavailable");
    }
    if (wantProject && wantProject !== names.projectName) return fail(c, 403, "forbidden", `this token belongs to ${names.projectName}`);
    return { projectId: names.projectId, projectName: names.projectName, canWrite: c.get("servicePermission") !== "read" };
  }

  const userId = c.get("userId");
  if (!wantProject) return fail(c, 400, "invalid_request", "a project is required");
  let project;
  try {
    project = await projects.findProjectByName(db, userId, wantProject);
  } catch (err) {
    return fail(c, 404, "project_not_found", (err as Error).message);
  }
  if (!(await projects.canView(db, userId, project.ID))) return forbidden(c);
  return { projectId: project.ID, projectName: project.Name, canWrite: await projects.canWrite(db, userId, project.ID) };
}

// GET /kv?project=
export async function listKv(c: Ctx) {
  const target = await kvTarget(c, c.req.query("project") ?? "");
  if (target instanceof Response) return target;
  try {
    return c.json({ project: target.projectName, values: await kv.allEntries(c.env.deps.db, c.env.deps.cipher, target.projectId) });
  } catch {
    return fail(c, 500, "internal", "values unavailable");
  }
}

// PUT /kv {project, key, value}
export async function setKv(c: Ctx) {
  const body = await readBody(c, { project: "string", key: "string", value: "string" });
  if (!body?.key) return fail(c, 400, "invalid_request", "key required");
  const target = await kvTarget(c, body.project);
  if (target instanceof Response) return target;
  if (!target.canWrite) return fail(c, 403, "forbidden", "this credential may only read");
  try {
    await kv.setEntry(c.env.deps.db, c.env.deps.cipher, target.projectId, body.key, body.value);
  } catch (err) {
    if (err instanceof Invalid) return fail(c, 400, "invalid_request", err.message);
    throw err;
  }
  return c.json({ project: target.projectName, key: body.key });
}

// DELETE /kv {project, key}
export async function deleteKv(c: Ctx) {
  const body = await readBody(c, { project: "string", key: "string" });
  if (!body?.key) return fail(c, 400, "invalid_request", "key required");
  const target = await kvTarget(c, body.project);
  if (target instanceof Response) return target;
  if (!target.canWrite) return fail(c, 403, "forbidden", "this credential may only read");
  try {
    await kv.deleteEntry(c.env.deps.db, target.projectId, body.key);
  } catch {
    return fail(c, 500, "internal", "unable to delete");
  }
  return c.body(null, 204);
}
