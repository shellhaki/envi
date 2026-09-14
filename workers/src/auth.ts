import { blob, changed, now, one, run, uuid } from "./db";
import { forbidden, tooMany, unauthorized } from "./errors";
import { hashToken, randomToken } from "./crypto";
import { ACCESS_TTL_SECONDS, REFRESH_TTL_SECONDS } from "./types";

export type TokenPair = { access_token: string; refresh_token: string; expires_in: number };

export async function createSession(db: D1Database, userId: string): Promise<TokenPair> {
  const access = randomToken();
  const refresh = randomToken();
  const t = now();
  await run(
    db,
    `INSERT INTO sessions(id,user_id,refresh_token_hash,access_token_hash,access_expires_at,expires_at,created_at)
     VALUES(?,?,?,?,?,?,?)`,
    uuid(),
    userId,
    blob(await hashToken(refresh)),
    blob(await hashToken(access)),
    t + ACCESS_TTL_SECONDS * 1000,
    t + REFRESH_TTL_SECONDS * 1000,
    t,
  );
  return { access_token: access, refresh_token: refresh, expires_in: ACCESS_TTL_SECONDS };
}

export async function authenticate(db: D1Database, access: string): Promise<string | null> {
  const row = await one<{ user_id: string }>(
    db,
    `SELECT user_id FROM sessions WHERE access_token_hash=? AND revoked_at IS NULL AND access_expires_at>?`,
    blob(await hashToken(access)),
    now(),
  );
  return row?.user_id ?? null;
}

/**
 * Refresh tokens are single use. D1 has no SELECT FOR UPDATE, so the atomic
 * step is the conditional UPDATE: whoever flips revoked_at first is the one
 * winner, and everyone else sees zero rows changed.
 */
export async function rotate(db: D1Database, refresh: string): Promise<TokenPair> {
  const hash = blob(await hashToken(refresh));
  const t = now();
  const session = await one<{ id: string; user_id: string }>(
    db,
    `SELECT id,user_id FROM sessions WHERE refresh_token_hash=? AND revoked_at IS NULL AND expires_at>?`,
    hash,
    t,
  );
  if (!session) throw unauthorized("invalid refresh token");

  const claimed = await changed(
    db,
    `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`,
    t,
    session.id,
  );
  if (claimed !== 1) throw unauthorized("invalid refresh token");
  return createSession(db, session.user_id);
}

export async function revokeSession(db: D1Database, refresh: string): Promise<void> {
  const count = await changed(
    db,
    `UPDATE sessions SET revoked_at=? WHERE refresh_token_hash=? AND revoked_at IS NULL`,
    now(),
    blob(await hashToken(refresh)),
  );
  if (count === 0) throw unauthorized("invalid refresh token");
}

export async function authenticateServiceToken(db: D1Database, token: string) {
  const row = await one<{ id: string; environment_id: string; permission: string }>(
    db,
    `SELECT si.id, si.environment_id, t.permission
     FROM api_tokens t JOIN service_identities si ON si.id=t.service_identity_id
     WHERE t.token_hash=? AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at>?)`,
    blob(await hashToken(token)),
    now(),
  );
  if (!row) return null;
  await run(db, `UPDATE api_tokens SET last_used_at=? WHERE token_hash=?`, now(), blob(await hashToken(token)));
  return row;
}

const OTP_TTL_MS = 10 * 60 * 1000;
const OTP_MAX_ATTEMPTS = 10;
const OTP_REQUEST_LIMIT = 20;
const OTP_REQUEST_WINDOW_MS = 60 * 60 * 1000;

export function randomCode(): string {
  const n = crypto.getRandomValues(new Uint32Array(1))[0]! % 1_000_000;
  return n.toString().padStart(6, "0");
}

export async function issueOtp(db: D1Database, email: string): Promise<string> {
  await checkRateLimit(db, `otp:${email}`, OTP_REQUEST_LIMIT, OTP_REQUEST_WINDOW_MS);
  const code = randomCode();
  const t = now();
  await run(
    db,
    `INSERT INTO otp_codes(email,code_hash,attempts,expires_at,created_at) VALUES(?,?,0,?,?)
     ON CONFLICT(email) DO UPDATE SET code_hash=excluded.code_hash, attempts=0, expires_at=excluded.expires_at, created_at=excluded.created_at`,
    email,
    blob(await hashToken(code)),
    t + OTP_TTL_MS,
    t,
  );
  return code;
}

export async function verifyOtp(db: D1Database, email: string, code: string): Promise<void> {
  const row = await one<{ code_hash: unknown; attempts: number }>(
    db,
    `SELECT code_hash,attempts FROM otp_codes WHERE email=? AND expires_at>?`,
    email,
    now(),
  );
  if (!row) throw unauthorized("invalid or expired OTP");
  if (row.attempts >= OTP_MAX_ATTEMPTS) throw unauthorized("invalid or expired OTP");

  // Counted before comparing, so a guess costs an attempt even if the request
  // is abandoned mid-flight.
  await run(db, `UPDATE otp_codes SET attempts=attempts+1 WHERE email=?`, email);

  if (!timingSafeEqual(toArray(row.code_hash), await hashToken(code))) {
    throw unauthorized("invalid or expired OTP");
  }
  await run(db, `DELETE FROM otp_codes WHERE email=?`, email);
}

function toArray(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (Array.isArray(value)) return new Uint8Array(value);
  return new Uint8Array();
}

function timingSafeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.byteLength !== b.byteLength) return false;
  let diff = 0;
  for (let i = 0; i < a.byteLength; i++) diff |= a[i]! ^ b[i]!;
  return diff === 0;
}

export async function checkRateLimit(db: D1Database, bucket: string, limit: number, windowMs: number): Promise<void> {
  const t = now();
  const row = await one<{ count: number; window_start: number }>(
    db,
    `SELECT count,window_start FROM rate_limits WHERE bucket=?`,
    bucket,
  );
  if (!row || t - row.window_start > windowMs) {
    await run(
      db,
      `INSERT INTO rate_limits(bucket,count,window_start) VALUES(?,1,?)
       ON CONFLICT(bucket) DO UPDATE SET count=1, window_start=excluded.window_start`,
      bucket,
      t,
    );
    return;
  }
  if (row.count >= limit) throw tooMany("too many requests; try again later");
  await run(db, `UPDATE rate_limits SET count=count+1 WHERE bucket=?`, bucket);
}

import { betaTesters } from "./beta";
import { ApiError } from "./errors";

/** Beta gating: only listed addresses may sign in when ENVIRONMENT=beta. */
export function assertInvited(environment: string, email: string) {
  if (environment !== "beta") return;
  if (!betaTesters.includes(email.trim().toLowerCase())) {
    throw new ApiError(403, "not_invited", "this email isn't on the beta list yet");
  }
}

export { forbidden };
