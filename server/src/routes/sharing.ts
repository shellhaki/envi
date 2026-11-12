import { Forbidden, Invalid, RateLimited } from "../errors";
import { fail, forbidden, readBody, param, type Ctx } from "../http";
import { Refusal } from "../services/billing";
import * as invitations from "../services/invitations";
import { createToken, revokeToken } from "../services/serviceTokens";

// POST /projects/:id/environments/:env/service-tokens {name, permission, ttl_seconds}
export async function createServiceToken(c: Ctx) {
  const body = await readBody(c, { name: "string", permission: "string", ttl_seconds: "number" });
  if (!body?.name) return fail(c, 400, "invalid_request", "name required");
  try {
    const token = await createToken(
      c.env.deps.db, c.get("userId"), param(c, "id"), param(c, "env"), body.name, body.permission || "read", body.ttl_seconds,
    );
    return c.json(token, 201);
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 409, "conflict", "token could not be created");
  }
}

// DELETE /service-tokens with the token to revoke in the X-Service-Token header.
export async function revokeServiceToken(c: Ctx) {
  const token = (c.req.header("X-Service-Token") ?? "").trim();
  if (!token) return fail(c, 400, "invalid_request", "service token required");
  try {
    await revokeToken(c.env.deps.db, c.get("userId"), token);
  } catch {
    return fail(c, 404, "not_found", "token not found");
  }
  return c.body(null, 204);
}

// GET /invitations/:token is public, so the accept page can show what the link is for before sign-in.
export async function previewInvitation(c: Ctx) {
  try {
    return c.json(await invitations.previewInvitation(c.env.deps, param(c, "token")));
  } catch (err) {
    if (err instanceof Forbidden) return fail(c, 404, "not_found", "invitation not found or expired");
    return fail(c, 500, "internal", "unable to load invitation");
  }
}

// POST /projects/:id/invitations {email, environment_id, permission, ttl_seconds}
export async function createInvitation(c: Ctx) {
  const body = await readBody(c, { email: "string", environment_id: "string", permission: "string", ttl_seconds: "number" });
  if (!body?.email) return fail(c, 400, "invalid_request", "email required");
  try {
    const invitation = await invitations.createInvitation(
      c.env.deps, c.get("userId"), param(c, "id"), body.environment_id, body.email, body.permission || "read", body.ttl_seconds,
    );
    return c.json(invitation, 201);
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    if (err instanceof Refusal) return c.json(err, 402);
    if (err instanceof RateLimited) return fail(c, 429, "rate_limited", err.message);
    if (err instanceof Invalid) return fail(c, 400, "invalid_request", err.message);
    return fail(c, 400, "invalid_request", (err as Error).message);
  }
}

// POST /invitations/accept {token}
export async function acceptInvitation(c: Ctx) {
  const body = await readBody(c, { token: "string" });
  if (!body?.token) return fail(c, 400, "invalid_request", "token required");
  try {
    await invitations.acceptInvitation(c.env.deps, c.get("userId"), body.token);
  } catch (err) {
    if (err instanceof Refusal) {
      err.message = "this invitation can't be accepted yet: the organization that sent it needs to update its plan";
      return c.json(err, 402);
    }
    return fail(c, 403, "forbidden", "invitation invalid or expired");
  }
  return c.body(null, 204);
}

// GET /projects/:id/collaborators
export async function listCollaborators(c: Ctx) {
  try {
    return c.json(await invitations.listCollaborators(c.env.deps, c.get("userId"), param(c, "id")));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "unable to load collaborators");
  }
}

// DELETE /projects/:id/invitations/:invitationID
export async function revokeInvitation(c: Ctx) {
  try {
    await invitations.revokeInvitation(c.env.deps, c.get("userId"), param(c, "id"), param(c, "invitationID"));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "unable to revoke invitation");
  }
  return c.body(null, 204);
}

// DELETE /projects/:id/collaborators/:grantID
export async function removeCollaborator(c: Ctx) {
  try {
    await invitations.removeCollaborator(c.env.deps, c.get("userId"), param(c, "id"), param(c, "grantID"));
  } catch (err) {
    if (err instanceof Forbidden) return forbidden(c);
    return fail(c, 500, "internal", "unable to remove collaborator");
  }
  return c.body(null, 204);
}
