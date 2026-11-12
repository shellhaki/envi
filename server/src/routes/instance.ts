// A self-hosted instance's sign-in and accounts. With email sign-in (no users file) these answer 404.

import type { MiddlewareHandler } from "hono";
import type { AppEnv } from "../deps";
import { Forbidden, Invalid, NotFound, RateLimited } from "../errors";
import { fail, forbidden, param, readBody, type Ctx } from "../http";
import * as members from "../services/members";
import { startSession, tokensReply } from "../services/sessions";

function failure(c: Ctx, err: unknown) {
  if (err instanceof Invalid) return fail(c, 400, "invalid_request", err.message);
  if (err instanceof NotFound) return fail(c, 404, "not_found", err.message);
  if (err instanceof Forbidden) return forbidden(c);
  throw err;
}

// GET /instance tells the dashboard and CLI how this server signs people in.
export function about(c: Ctx) {
  const { config, users, bachs } = c.env.deps;
  return c.json({ mode: config.mode, sign_in: users ? "password" : "email", billing: bachs !== null });
}

// POST /auth/password {email, password}
export async function signIn(c: Ctx) {
  const deps = c.env.deps;
  if (!deps.users) return fail(c, 404, "not_found", "this instance signs in by email, not with a password");
  const body = await readBody(c, { email: "string", password: "string" });
  if (!body?.email || !body.password) return fail(c, 400, "invalid_request", "email and password required");
  try {
    const userId = await members.signInWithPassword(deps, body.email, body.password);
    return c.json(tokensReply(await startSession(deps.db, userId)));
  } catch (err) {
    if (err instanceof RateLimited) return fail(c, 429, "rate_limited", err.message);
    if (err instanceof Forbidden) return fail(c, 401, "invalid_credentials", "wrong email or password");
    throw err;
  }
}

// POST /me/password {current_password, new_password}
export async function changePassword(c: Ctx) {
  const body = await readBody(c, { current_password: "string", new_password: "string" });
  if (!body?.current_password || !body.new_password) return fail(c, 400, "invalid_request", "current_password and new_password required");
  try {
    await members.changeOwnPassword(c.env.deps, c.get("userId"), body.current_password, body.new_password);
  } catch (err) {
    if (err instanceof Forbidden) return fail(c, 403, "wrong_password", "the current password is wrong");
    return failure(c, err);
  }
  return c.body(null, 204);
}

// instanceAdmin lets through only the admin named in the users file.
export const instanceAdmin: MiddlewareHandler<AppEnv> = async (c, next) => {
  if (!c.env.deps.users) return fail(c, 404, "not_found", "this instance signs in by email, not with a password");
  if ((await members.roleOf(c.env.deps, c.get("userId"))) !== "admin") {
    return fail(c, 403, "forbidden", "only this instance's admin can manage its users");
  }
  return next();
};

// GET /instance/users
export async function listUsers(c: Ctx) {
  return c.json(await members.listMembers(c.env.deps));
}

// POST /instance/users {email}. The generated password is in the reply and nowhere else.
export async function addUser(c: Ctx) {
  const body = await readBody(c, { email: "string" });
  if (!body?.email) return fail(c, 400, "invalid_request", "email required");
  try {
    return c.json(await members.addMember(c.env.deps, body.email), 201);
  } catch (err) {
    return failure(c, err);
  }
}

// POST /instance/users/:id/password gives the user a new generated password.
export async function resetPassword(c: Ctx) {
  try {
    return c.json(await members.resetMemberPassword(c.env.deps, param(c, "id")));
  } catch (err) {
    return failure(c, err);
  }
}

// DELETE /instance/users/:id
export async function removeUser(c: Ctx) {
  try {
    await members.removeMember(c.env.deps, param(c, "id"));
  } catch (err) {
    return failure(c, err);
  }
  return c.body(null, 204);
}

// POST /projects/:id/collaborators {user_id, permission, environment_id} gives a user of this instance a project.
export async function grantAccess(c: Ctx) {
  if (!c.env.deps.users) return fail(c, 404, "not_found", "on this instance, people are invited by email");
  const body = await readBody(c, { user_id: "string", permission: "string", environment_id: "string" });
  if (!body?.user_id) return fail(c, 400, "invalid_request", "user_id required");
  try {
    const grant = await members.grantAccess(
      c.env.deps, c.get("userId"), param(c, "id"), body.user_id, body.permission || "read", body.environment_id,
    );
    return c.json(grant, 201);
  } catch (err) {
    return failure(c, err);
  }
}
