import type { MiddlewareHandler } from "hono";
import { normaliseEmail } from "./config";
import type { AppEnv } from "./deps";
import { route, type Ctx } from "./http";
import * as admin from "./services/admin";
import { userForAccessToken } from "./services/sessions";
import { authenticateToken } from "./services/serviceTokens";

type Middleware = MiddlewareHandler<AppEnv>;

// signedIn accepts a user's access token or a service token, and answers 401 to anything else.
export const signedIn: Middleware = async (c, next) => {
  const { db } = c.env.deps;
  const token = (c.req.header("Authorization") ?? "").replace(/^Bearer /, "");

  const user = await userForAccessToken(db, token);
  if (user) {
    const reason = blockedByCeiling(c, user.permission);
    if (reason) return c.json({ code: "insufficient_permission", error: reason }, 403);
    c.set("userId", user.userId);
    c.set("userPermission", user.permission);
    return next();
  }

  const service = await authenticateToken(db, token);
  if (service) {
    c.set("serviceId", service.serviceId);
    c.set("serviceEnv", service.envId);
    c.set("servicePermission", service.permission);
    return next();
  }
  return c.json({ code: "unauthenticated", error: "authentication required" }, 401);
};

// blockedByCeiling enforces the limit an API key puts on its session, returning why it refuses or "".
// A read key may only read; a write key may not hand out access or create credentials.
function blockedByCeiling(c: Ctx, ceiling: string): string {
  if (ceiling === "read" && c.req.method !== "GET" && c.req.method !== "HEAD") return "this api key is read-only";
  if (ceiling === "write" && ["/api-keys", "/invitations", "/service-tokens", "/collaborators"].some((p) => route(c).includes(p))) {
    return "this api key may not manage access or credentials";
  }
  return "";
}

// operator lets through only operators (ENVI_ADMINS or the operators table). Everyone else gets an empty 404.
export const operator: Middleware = async (c, next) => {
  const { db, config } = c.env.deps;
  const userId = c.get("userId");
  if (!userId) return c.body(null, 404);
  const user = await db.one(`SELECT email FROM users WHERE id = $1`, [userId]).catch(() => undefined);
  if (!user) return c.body(null, 404);
  const role = await admin.roleFor(db, user.email, config.admins);
  if (!role) return c.body(null, 404);
  c.set("operatorRole", role);
  c.set("operatorEmail", normaliseEmail(user.email));
  return next();
};

// canManage lets through admins only (not announcers).
export const canManage: Middleware = (c, next) => (admin.mayManage(c.get("operatorRole")) ? next() : Promise.resolve(c.body(null, 404)));

// canAnnounce lets through admins and announcers.
export const canAnnounce: Middleware = (c, next) => (admin.mayAnnounce(c.get("operatorRole")) ? next() : Promise.resolve(c.body(null, 404)));
