// Device login: the CLI gets a secret device code and a short user code, the user approves
// the user code on the website, and the CLI polls with its device code until it's approved.

import { hashToken, randomBelow, randomToken } from "../crypto";
import type { Db } from "../db";
import { startSession, type Tokens } from "./sessions";

export const DEVICE_CODE_SECONDS = 10 * 60;
export const DEVICE_POLL_SECONDS = 5;

// 32 letters, without I, O, 0 and 1, which are easy to misread.
const USER_CODE_LETTERS = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";

// DevicePending means the CLI isn't signed in yet: "authorization_pending", "access_denied" or "expired_token".
export class DevicePending extends Error {
  constructor(public reason: string) {
    super(reason);
  }
}

export async function startDeviceLogin(db: Db) {
  const deviceCode = randomToken();
  const deviceHash = await hashToken(deviceCode);
  let lastError: unknown;
  // User codes must be unique among live logins; on the rare clash, try another.
  for (let attempt = 0; attempt < 5; attempt++) {
    const code = newUserCode();
    try {
      await db.exec(
        `INSERT INTO device_authorizations (device_code_hash, user_code, expires_at)
         VALUES ($1, $2, now() + make_interval(secs => $3))`,
        [deviceHash, code, DEVICE_CODE_SECONDS],
      );
      return { deviceCode, userCode: `${code.slice(0, 4)}-${code.slice(4)}` };
    } catch (err) {
      lastError = err;
    }
  }
  throw lastError;
}

export async function approveDeviceLogin(db: Db, userCode: string, userId: string): Promise<boolean> {
  const changed = await db.exec(
    `UPDATE device_authorizations SET status = 'approved', user_id = $2, approved_at = now()
      WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
    [normalizeUserCode(userCode), userId],
  );
  return changed > 0;
}

export async function denyDeviceLogin(db: Db, userCode: string): Promise<boolean> {
  const changed = await db.exec(
    `UPDATE device_authorizations SET status = 'denied'
      WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
    [normalizeUserCode(userCode)],
  );
  return changed > 0;
}

// finishDeviceLogin returns tokens once approved, and marks the code used so it signs in only once.
export async function finishDeviceLogin(db: Db, deviceCode: string): Promise<Tokens> {
  const hash = await hashToken(deviceCode);
  const userId = await db.transaction(async (tx) => {
    const row = await tx.one(
      `SELECT id, status, user_id, expires_at < now() AS expired
         FROM device_authorizations WHERE device_code_hash = $1 FOR UPDATE`,
      [hash],
    );
    if (!row) throw new Error("device code not found");
    if (row.expired) throw new DevicePending("expired_token");
    if (row.status === "pending" || (row.status === "approved" && !row.user_id)) throw new DevicePending("authorization_pending");
    if (row.status === "denied") throw new DevicePending("access_denied");
    if (row.status !== "approved") throw new DevicePending("expired_token"); // already redeemed
    await tx.exec(`UPDATE device_authorizations SET status = 'redeemed' WHERE id = $1`, [row.id]);
    return row.user_id as string;
  });
  return startSession(db, userId);
}

function newUserCode(): string {
  let code = "";
  for (let i = 0; i < 8; i++) code += USER_CODE_LETTERS[randomBelow(USER_CODE_LETTERS.length)];
  return code;
}

// normalizeUserCode turns "wxyz-abcd" or "WXYZ ABCD" into "WXYZABCD".
export function normalizeUserCode(typed: string): string {
  return [...typed.toUpperCase()].filter((ch) => USER_CODE_LETTERS.includes(ch)).join("");
}
