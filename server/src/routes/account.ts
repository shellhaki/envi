import { Forbidden, Invalid, NotFound } from "../errors";
import { fail, forbidden, readBody, param, type Ctx } from "../http";
import { createKey, DEFAULT_LIFETIME_SECONDS, listKeys, revokeKey } from "../services/apiKeys";
import { listEvents } from "../services/audit";
import { roleOf } from "../services/members";

// GET /me returns the signed-in user, their personal organization, and their role on a self-hosted instance.
export async function me(c: Ctx) {
  const row = await c.env.deps.db
    .one(
      `SELECT u.id AS "ID", u.email AS "Email", o.id AS "OrganizationID"
         FROM users u
         JOIN memberships m ON m.user_id = u.id
         JOIN organizations o ON o.id = m.org_id AND o.type = 'personal'
        WHERE u.id = $1`,
      [c.get("userId") || null],
    )
    .catch(() => undefined);
  if (!row) return fail(c, 500, "internal", "account unavailable");
  return c.json({ ...row, Role: await roleOf(c.env.deps, c.get("userId")) });
}

// POST /me/api-keys {name, permission, ttl_seconds}. The key's secret is only ever returned here.
// A missing ttl_seconds means the default lifetime; 0 means it never expires.
export async function createApiKey(c: Ctx) {
  const body = await readBody(c, { name: "string", permission: "string", ttl_seconds: "number?" });
  if (!body?.name) return fail(c, 400, "invalid_request", "name required");
  try {
    const key = await createKey(
      c.env.deps.db, c.get("userId"), body.name, body.permission || "read", body.ttl_seconds ?? DEFAULT_LIFETIME_SECONDS,
    );
    return c.json(key, 201);
  } catch (err) {
    if (err instanceof Invalid) return fail(c, 400, "invalid_request", err.message);
    throw err;
  }
}

// GET /me/api-keys
export async function listApiKeys(c: Ctx) {
  try {
    return c.json(await listKeys(c.env.deps.db, c.get("userId")));
  } catch {
    return c.json({ error: "unable to list api keys" }, 500);
  }
}

// DELETE /me/api-keys/:id
export async function revokeApiKey(c: Ctx) {
  try {
    await revokeKey(c.env.deps.db, c.get("userId"), param(c, "id"));
  } catch (err) {
    if (err instanceof NotFound) return fail(c, 404, "not_found", "api key not found");
    return c.json({ error: "unable to revoke api key" }, 500);
  }
  return c.body(null, 204);
}

// GET /orgs/:id/audit-events
export async function listAuditEvents(c: Ctx) {
  try {
    return c.json(await listEvents(c.env.deps.db, c.get("userId"), param(c, "id")));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "audit events unavailable");
  }
}
