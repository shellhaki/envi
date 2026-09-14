import { blob, changed, now, one, run, uuid } from "./db";
import { hashToken, randomToken } from "./crypto";
import { ApiError } from "./errors";
import { createSession, type TokenPair } from "./auth";

const TTL_MS = 10 * 60 * 1000;
const INTERVAL_SECONDS = 5;
const ALPHABET = "BCDFGHJKLMNPQRSTVWXZ23456789"; // no vowels, no 0/1/O/I

export async function start(db: D1Database) {
  const deviceCode = randomToken();
  const userCode = `${block(4)}-${block(4)}`;
  const t = now();
  await run(
    db,
    `INSERT INTO device_authorizations(id,device_code_hash,user_code,status,expires_at,created_at)
     VALUES(?,?,?, 'pending',?,?)`,
    uuid(),
    blob(await hashToken(deviceCode)),
    userCode,
    t + TTL_MS,
    t,
  );
  return {
    device_code: deviceCode,
    user_code: userCode,
    expires_in: Math.floor(TTL_MS / 1000),
    interval: INTERVAL_SECONDS,
  };
}

function block(n: number): string {
  const bytes = crypto.getRandomValues(new Uint8Array(n));
  return [...bytes].map((b) => ALPHABET[b % ALPHABET.length]).join("");
}

/** RFC 8628 poll codes; the CLI branches on `code`, not the message. */
export async function redeem(db: D1Database, deviceCode: string): Promise<TokenPair> {
  const row = await one<{ id: string; status: string; user_id: string | null; expires_at: number }>(
    db,
    `SELECT id,status,user_id,expires_at FROM device_authorizations WHERE device_code_hash=?`,
    blob(await hashToken(deviceCode)),
  );
  if (!row) throw new ApiError(400, "invalid_grant", "invalid device code");
  if (row.expires_at <= now()) throw new ApiError(400, "expired_token", "the code expired");

  switch (row.status) {
    case "pending":
      throw new ApiError(400, "authorization_pending", "waiting for you to approve the code in your browser");
    case "denied":
      throw new ApiError(400, "access_denied", "the request was denied");
    case "redeemed":
      throw new ApiError(400, "invalid_grant", "invalid device code");
  }

  // Single use: only the caller that flips approved -> redeemed gets tokens.
  const claimed = await changed(
    db,
    `UPDATE device_authorizations SET status='redeemed' WHERE id=? AND status='approved'`,
    row.id,
  );
  if (claimed !== 1 || !row.user_id) throw new ApiError(400, "invalid_grant", "invalid device code");
  return createSession(db, row.user_id);
}

export async function approve(db: D1Database, userCode: string, userId: string): Promise<void> {
  const count = await changed(
    db,
    `UPDATE device_authorizations SET status='approved', user_id=?, approved_at=?
     WHERE user_code=? AND status='pending' AND expires_at>?`,
    userId,
    now(),
    userCode.trim().toUpperCase(),
    now(),
  );
  if (count !== 1) throw new ApiError(400, "invalid_grant", "that code is invalid or expired");
}

export async function deny(db: D1Database, userCode: string): Promise<void> {
  const count = await changed(
    db,
    `UPDATE device_authorizations SET status='denied' WHERE user_code=? AND status='pending' AND expires_at>?`,
    userCode.trim().toUpperCase(),
    now(),
  );
  if (count !== 1) throw new ApiError(400, "invalid_grant", "that code is invalid or expired");
}
