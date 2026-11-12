import { hashToken, randomToken } from "../crypto";
import type { Db } from "../db";

export const ACCESS_TOKEN_SECONDS = 15 * 60;
export const REFRESH_TOKEN_SECONDS = 30 * 24 * 60 * 60;

export type Tokens = { access: string; refresh: string };

// startSession creates an access and refresh token pair. A permission caps what the session may do (API keys).
export async function startSession(db: Db, userId: string, permission = "", apiKeyId = ""): Promise<Tokens> {
  const access = randomToken();
  const refresh = randomToken();
  await db.exec(
    `INSERT INTO sessions (user_id, refresh_token_hash, access_token_hash, access_expires_at, expires_at, permission, api_key_id)
     VALUES ($1, $2, $3, now() + make_interval(secs => $4), now() + make_interval(secs => $5), $6, $7)`,
    [userId, await hashToken(refresh), await hashToken(access), ACCESS_TOKEN_SECONDS, REFRESH_TOKEN_SECONDS, permission || null, apiKeyId || null],
  );
  return { access, refresh };
}

// refreshSession swaps a refresh token for a new pair. Each refresh token works once.
export async function refreshSession(db: Db, refreshToken: string): Promise<Tokens> {
  const hash = await hashToken(refreshToken);
  const session = await db.transaction(async (tx) => {
    const row = await tx.one(
      `SELECT id, user_id, permission, api_key_id FROM sessions
        WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
          FOR UPDATE`,
      [hash],
    );
    if (!row) throw new Error("invalid refresh token");
    await tx.exec(`UPDATE sessions SET revoked_at = now() WHERE id = $1`, [row.id]);
    return row;
  });
  return startSession(db, session.user_id, session.permission ?? "", session.api_key_id ?? "");
}

export async function endSession(db: Db, refreshToken: string): Promise<void> {
  await db.exec(`UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1`, [await hashToken(refreshToken)]);
}

// userForAccessToken returns who an access token belongs to and its permission ceiling ("" for none).
export async function userForAccessToken(db: Db, accessToken: string) {
  const row = await db.one(
    `SELECT user_id, permission FROM sessions
      WHERE access_token_hash = $1 AND revoked_at IS NULL AND access_expires_at > now()`,
    [await hashToken(accessToken)],
  );
  if (!row) return undefined;
  return { userId: row.user_id as string, permission: (row.permission ?? "") as string };
}

export function tokensReply(tokens: Tokens) {
  return { access_token: tokens.access, refresh_token: tokens.refresh, expires_in: ACCESS_TOKEN_SECONDS };
}
