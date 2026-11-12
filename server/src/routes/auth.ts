import { fail, isEmail, readBody, type Ctx } from "../http";
import { RateLimited } from "../errors";
import { authenticateKey } from "../services/apiKeys";
import { approveDeviceLogin, denyDeviceLogin, DevicePending, DEVICE_CODE_SECONDS, DEVICE_POLL_SECONDS, finishDeviceLogin, startDeviceLogin } from "../services/device";
import { checkLoginCode, sendLoginCode } from "../services/login";
import { endSession, refreshSession, startSession, tokensReply } from "../services/sessions";

const usesPassword = (c: Ctx) => fail(c, 404, "password_sign_in", "this instance signs in with a password: POST /auth/password");

// POST /auth/request-otp {email}
export async function requestLoginCode(c: Ctx) {
  if (c.env.deps.users) return usesPassword(c);
  const body = await readBody(c, { email: "string" });
  if (!body || !isEmail(body.email)) return c.json({ error: "valid email required" }, 400);
  try {
    await sendLoginCode(c.env.deps, body.email);
  } catch (err) {
    console.error(`otp delivery failed: ${(err as Error).message}`);
    if (err instanceof RateLimited) return fail(c, 429, "rate_limited", "too many OTP requests");
    return c.json({ error: "unable to send code" }, 500);
  }
  return c.body(null, 202);
}

// POST /auth/verify-otp {email, code}
export async function verifyLoginCode(c: Ctx) {
  if (c.env.deps.users) return usesPassword(c);
  const body = await readBody(c, { email: "string", code: "string" });
  if (!body || !isEmail(body.email) || !body.code) return c.json({ error: "email and code required" }, 400);
  let userId: string;
  try {
    userId = await checkLoginCode(c.env.deps, body.email, body.code);
  } catch (err) {
    console.error(`otp verify failed: ${(err as Error).message}`);
    return c.json({ error: "invalid or expired OTP" }, 401);
  }
  return c.json(tokensReply(await startSession(c.env.deps.db, userId)));
}

// POST /auth/refresh {refresh_token}
export async function refresh(c: Ctx) {
  const body = await readBody(c, { refresh_token: "string" });
  if (!body?.refresh_token) return c.json({ error: "refresh_token required" }, 400);
  try {
    return c.json(tokensReply(await refreshSession(c.env.deps.db, body.refresh_token)));
  } catch {
    return c.json({ error: "invalid refresh token" }, 401);
  }
}

// POST /auth/logout {refresh_token}
export async function logout(c: Ctx) {
  const body = await readBody(c, { refresh_token: "string" });
  if (!body?.refresh_token) return c.json({ error: "refresh_token required" }, 400);
  try {
    await endSession(c.env.deps.db, body.refresh_token);
  } catch {
    return c.json({ error: "invalid refresh token" }, 401);
  }
  return c.body(null, 204);
}

// POST /auth/api-key {key} trades a personal API key for a session capped at the key's permission.
export async function exchangeApiKey(c: Ctx) {
  const body = await readBody(c, { key: "string" });
  if (!body?.key) return fail(c, 400, "invalid_request", "key required");
  const key = await authenticateKey(c.env.deps.db, body.key);
  if (!key) return fail(c, 401, "invalid_key", "invalid or expired api key");
  return c.json(tokensReply(await startSession(c.env.deps.db, key.userId, key.permission, key.keyId)));
}

// POST /auth/device/code, called by the CLI to start a browser login.
export async function startDevice(c: Ctx) {
  let codes;
  try {
    codes = await startDeviceLogin(c.env.deps.db);
  } catch {
    return fail(c, 500, "server_error", "unable to start device authorization");
  }
  const page = c.env.deps.config.webUrl.replace(/\/+$/, "") + "/device";
  return c.json({
    device_code: codes.deviceCode,
    user_code: codes.userCode,
    verification_uri: page,
    verification_uri_complete: `${page}?code=${encodeURIComponent(codes.userCode)}`,
    expires_in: DEVICE_CODE_SECONDS,
    interval: DEVICE_POLL_SECONDS,
  });
}

const deviceMessages: Record<string, string> = {
  authorization_pending: "waiting for you to approve the code in your browser",
  slow_down: "polling too fast",
  access_denied: "the request was denied",
  expired_token: "the code expired",
};

// POST /auth/device/token {device_code}, polled by the CLI until the browser approves.
export async function finishDevice(c: Ctx) {
  const body = await readBody(c, { device_code: "string" });
  if (!body?.device_code) return fail(c, 400, "invalid_request", "device_code required");
  try {
    return c.json(tokensReply(await finishDeviceLogin(c.env.deps.db, body.device_code)));
  } catch (err) {
    if (err instanceof DevicePending) {
      return fail(c, 400, err.reason, deviceMessages[err.reason] ?? "device authorization not ready");
    }
    return fail(c, 400, "invalid_grant", "invalid device code");
  }
}

// POST /auth/device/approve {user_code}, clicked on the website.
export async function approveDevice(c: Ctx) {
  const body = await readBody(c, { user_code: "string" });
  if (!body?.user_code) return fail(c, 400, "invalid_request", "user_code required");
  const userId = c.get("userId");
  if (!userId || !(await approveDeviceLogin(c.env.deps.db, body.user_code, userId))) {
    return fail(c, 400, "invalid_grant", "that code is invalid or expired");
  }
  return c.body(null, 204);
}

// POST /auth/device/deny {user_code}, clicked on the website.
export async function denyDevice(c: Ctx) {
  const body = await readBody(c, { user_code: "string" });
  if (!body?.user_code) return fail(c, 400, "invalid_request", "user_code required");
  if (!(await denyDeviceLogin(c.env.deps.db, body.user_code))) {
    return fail(c, 400, "invalid_grant", "that code is invalid or expired");
  }
  return c.body(null, 204);
}
