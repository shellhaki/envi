import { Hono } from "hono";
import type { Context, Next } from "hono";

import { allow } from "./access";
import { assertLinkableWebUrl } from "./config";
import {
  assertInvited,
  authenticate,
  authenticateServiceToken,
  createSession,
  issueOtp,
  revokeSession,
  rotate,
  verifyOtp,
} from "./auth";
import { importKey } from "./crypto";
import { one } from "./db";
import * as device from "./device";
import { ApiError, badRequest, fail, forbidden, unauthorized } from "./errors";
import * as invitations from "./invitations";
import { otpEmail, send } from "./mail";
import * as projects from "./projects";
import * as secrets from "./secrets";
import * as tokens from "./tokens";
import { ACCESS_TTL_SECONDS, type Actor, type Env, type Permission, type Vars } from "./types";
import { provision } from "./workspace";

const app = new Hono<{ Bindings: Env; Variables: Vars }>();

app.onError((err, c) => fail(c, err));
app.notFound((c) => c.json({ code: "not_found", error: "not found" }, 404));

const key = (env: Env) => importKey(env.ENVI_ENCRYPTION_KEY);

const body = async <T>(c: Context): Promise<T> => {
  try {
    return (await c.req.json()) as T;
  } catch {
    throw badRequest("invalid JSON body");
  }
};

const param = (c: Context, name: string): string => {
  const value = c.req.param(name);
  if (!value) throw badRequest(`${name} required`);
  return value;
};

async function requireAuth(c: Context<{ Bindings: Env; Variables: Vars }>, next: Next) {
  const token = (c.req.header("Authorization") ?? "").replace(/^Bearer\s+/i, "").trim();
  if (!token) throw unauthorized();

  const userId = await authenticate(c.env.DB, token);
  if (userId) {
    c.set("actor", { kind: "user", userId } satisfies Actor);
    return next();
  }
  const service = await authenticateServiceToken(c.env.DB, token);
  if (service) {
    c.set("actor", {
      kind: "service",
      serviceId: service.id,
      environmentId: service.environment_id,
      permission: service.permission as Permission,
    } satisfies Actor);
    return next();
  }
  throw unauthorized();
}

const userId = (c: Context<{ Bindings: Env; Variables: Vars }>): string => {
  const actor = c.get("actor");
  if (actor.kind !== "user") throw forbidden();
  return actor.userId;
};

app.get("/health", (c) => c.json({ status: "ok" }));
app.get("/config", (c) => c.json({ beta: c.env.ENVIRONMENT === "beta" }));

/* ---------------------------------------------------------------- auth --- */

app.post("/auth/request-otp", async (c) => {
  const { email } = await body<{ email?: string }>(c);
  if (!email || !email.includes("@")) throw badRequest("valid email required");
  assertInvited(c.env.ENVIRONMENT, email);

  const code = await issueOtp(c.env.DB, email.trim().toLowerCase());
  try {
    await send(c.env, { ...otpEmail(code), to: email });
  } catch (err) {
    console.error("otp delivery failed:", err);
    throw new ApiError(500, "internal", "unable to send code");
  }
  return c.body(null, 202);
});

app.post("/auth/verify-otp", async (c) => {
  const { email, code } = await body<{ email?: string; code?: string }>(c);
  if (!email || !code) throw badRequest("email and code required");
  assertInvited(c.env.ENVIRONMENT, email);

  const normalized = email.trim().toLowerCase();
  await verifyOtp(c.env.DB, normalized, code);
  const { userId: id } = await provision(c.env.DB, normalized);
  return c.json(await createSession(c.env.DB, id));
});

app.post("/auth/refresh", async (c) => {
  const { refresh_token } = await body<{ refresh_token?: string }>(c);
  if (!refresh_token) throw badRequest("refresh_token required");
  return c.json(await rotate(c.env.DB, refresh_token));
});

app.post("/auth/logout", async (c) => {
  const { refresh_token } = await body<{ refresh_token?: string }>(c);
  if (!refresh_token) throw badRequest("refresh_token required");
  await revokeSession(c.env.DB, refresh_token);
  return c.body(null, 204);
});

/* -------------------------------------------------------------- device --- */

app.post("/auth/device/code", async (c) => {
  assertLinkableWebUrl(c.env);
  const started = await device.start(c.env.DB);
  const verify = `${c.env.ENVI_WEB_URL.replace(/\/+$/, "")}/device`;
  return c.json({
    ...started,
    verification_uri: verify,
    verification_uri_complete: `${verify}?code=${encodeURIComponent(started.user_code)}`,
  });
});

app.post("/auth/device/token", async (c) => {
  const { device_code } = await body<{ device_code?: string }>(c);
  if (!device_code) throw badRequest("device_code required");
  const pair = await device.redeem(c.env.DB, device_code);
  return c.json({ ...pair, expires_in: ACCESS_TTL_SECONDS });
});

app.post("/auth/device/approve", requireAuth, async (c) => {
  const { user_code } = await body<{ user_code?: string }>(c);
  if (!user_code) throw badRequest("user_code required");
  await device.approve(c.env.DB, user_code, userId(c));
  return c.body(null, 204);
});

app.post("/auth/device/deny", requireAuth, async (c) => {
  const { user_code } = await body<{ user_code?: string }>(c);
  if (!user_code) throw badRequest("user_code required");
  await device.deny(c.env.DB, user_code);
  return c.body(null, 204);
});

/* ------------------------------------------------------------- account --- */

app.get("/me", requireAuth, async (c) => {
  const row = await one<{ ID: string; Email: string; OrganizationID: string }>(
    c.env.DB,
    `SELECT u.id AS ID, u.email AS Email, o.id AS OrganizationID FROM users u
     JOIN memberships m ON m.user_id=u.id
     JOIN organizations o ON o.id=m.org_id AND o.type='personal'
     WHERE u.id=?`,
    userId(c),
  );
  if (!row) throw new ApiError(500, "internal", "account unavailable");
  return c.json(row);
});

/* ------------------------------------------------------------ projects --- */

app.get("/projects", requireAuth, async (c) => c.json(await projects.list(c.env.DB, userId(c))));

app.post("/projects", requireAuth, async (c) => {
  const { org_id, name } = await body<{ org_id?: string; name?: string }>(c);
  if (!org_id || !name) throw badRequest("org_id and name required");
  return c.json(await projects.create(c.env.DB, userId(c), org_id, name), 201);
});

app.delete("/projects/:id", requireAuth, async (c) => {
  await projects.remove(c.env.DB, userId(c), param(c, "id"));
  return c.body(null, 204);
});

app.get("/projects/:id/environments", requireAuth, async (c) =>
  c.json(await projects.listEnvironments(c.env.DB, userId(c), param(c, "id"))),
);

app.post("/projects/:id/environments", requireAuth, async (c) => {
  const { name, is_production } = await body<{ name?: string; is_production?: boolean }>(c);
  if (!name) throw badRequest("name required");
  const env = await projects.createEnvironment(c.env.DB, userId(c), param(c, "id"), name, Boolean(is_production));
  return c.json(env, 201);
});

/* ------------------------------------------------------------- secrets --- */

app.get("/environments/:id/secrets/snapshot", requireAuth, async (c) => {
  const actor = c.get("actor");
  const envId = param(c, "id");
  if (actor.kind === "service") {
    if (actor.environmentId !== envId) throw forbidden();
    return c.json(await secrets.serviceSnapshot(c.env, await key(c.env), envId));
  }
  return c.json(await secrets.readSnapshot(c.env, await key(c.env), actor.userId, envId));
});

app.put("/environments/:id/secrets/snapshot", requireAuth, async (c) => {
  const actor = c.get("actor");
  const envId = param(c, "id");
  const { values, expected_revision } = await body<{ values?: Record<string, string>; expected_revision?: number }>(c);
  if (!values) throw badRequest("secret snapshot required");

  if (actor.kind === "service") {
    if (actor.environmentId !== envId || actor.permission === "read") throw forbidden();
    return c.json({ revision: await secrets.writeSnapshot(c.env, await key(c.env), null, envId, values, expected_revision ?? 0) });
  }
  return c.json({
    revision: await secrets.writeSnapshot(c.env, await key(c.env), actor.userId, envId, values, expected_revision ?? 0),
  });
});

app.get("/environments/:id/secrets", requireAuth, async (c) => {
  const actor = c.get("actor");
  const envId = param(c, "id");
  if (actor.kind === "service") {
    if (actor.environmentId !== envId) throw forbidden();
    return c.json((await secrets.serviceSnapshot(c.env, await key(c.env), envId)).values);
  }
  return c.json((await secrets.readSnapshot(c.env, await key(c.env), actor.userId, envId)).values);
});

app.put("/environments/:id/secrets", requireAuth, async (c) => {
  const actor = c.get("actor");
  const envId = param(c, "id");
  const values = await body<Record<string, string>>(c);
  if (actor.kind === "service") {
    if (actor.environmentId !== envId || actor.permission === "read") throw forbidden();
  } else {
    await allow(c.env.DB, actor.userId, envId, "write");
  }
  const current = await secrets.revision(c.env.DB, envId);
  const merged = { ...(await secrets.snapshot(c.env, await key(c.env), envId)).values, ...values };
  await secrets.writeSnapshot(c.env, await key(c.env), actor.kind === "user" ? actor.userId : null, envId, merged, current);
  return c.body(null, 204);
});

app.delete("/environments/:id/secrets/:secretKey", requireAuth, async (c) => {
  await secrets.remove(c.env, userId(c), param(c, "id"), decodeURIComponent(param(c, "secretKey")));
  return c.body(null, 204);
});

/* --------------------------------------------------------- invitations --- */

app.get("/invitations/:token", async (c) => c.json(await invitations.preview(c.env.DB, param(c, "token"))));

app.post("/projects/:id/invitations", requireAuth, async (c) => {
  const { email, environment_id, permission, ttl_seconds } = await body<{
    email?: string;
    environment_id?: string;
    permission?: Permission;
    ttl_seconds?: number;
  }>(c);
  if (!email) throw badRequest("email required");
  const invite = await invitations.create(
    c.env,
    userId(c),
    param(c, "id"),
    environment_id ?? "",
    email,
    permission ?? "read",
    ttl_seconds ?? 0,
  );
  return c.json(invite, 201);
});

app.post("/invitations/accept", requireAuth, async (c) => {
  const { token } = await body<{ token?: string }>(c);
  if (!token) throw badRequest("token required");
  await invitations.accept(c.env.DB, userId(c), token);
  return c.body(null, 204);
});

app.get("/projects/:id/collaborators", requireAuth, async (c) =>
  c.json(await invitations.listCollaborators(c.env.DB, userId(c), param(c, "id"))),
);

app.delete("/projects/:id/invitations/:invitationId", requireAuth, async (c) => {
  await invitations.revokeInvitation(c.env.DB, userId(c), param(c, "id"), param(c, "invitationId"));
  return c.body(null, 204);
});

app.delete("/projects/:id/collaborators/:grantId", requireAuth, async (c) => {
  await invitations.revokeGrant(c.env.DB, userId(c), param(c, "id"), param(c, "grantId"));
  return c.body(null, 204);
});

/* -------------------------------------------------- tokens and activity --- */

app.post("/projects/:id/environments/:env/service-tokens", requireAuth, async (c) => {
  const { name, permission, ttl_seconds } = await body<{ name?: string; permission?: Permission; ttl_seconds?: number }>(c);
  if (!name) throw badRequest("name required");
  const token = await tokens.createServiceToken(
    c.env.DB,
    userId(c),
    param(c, "id"),
    param(c, "env"),
    name,
    permission ?? "read",
    ttl_seconds ?? 0,
  );
  return c.json(token, 201);
});

app.delete("/service-tokens", requireAuth, async (c) => {
  const token = (c.req.header("X-Service-Token") ?? "").trim();
  if (!token) throw badRequest("service token required");
  await tokens.revokeServiceToken(c.env.DB, userId(c), token);
  return c.body(null, 204);
});

app.get("/orgs/:id/audit-events", requireAuth, async (c) => {
  const limit = Number(c.req.query("limit") ?? 50);
  return c.json(await tokens.listAuditEvents(c.env.DB, userId(c), param(c, "id"), limit));
});

export default app;
