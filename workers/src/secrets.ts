import { allow } from "./access";
import { blob, changed, now, one, all, run, toBytes, uuid } from "./db";
import { conflict, notFound } from "./errors";
import { open, seal } from "./crypto";
import type { Env } from "./types";

export type Snapshot = { values: Record<string, string>; revision: number };

/**
 * Cache keys embed the revision, so an entry is immutable: a write bumps the
 * revision and simply stops anyone reading the old key. That is what makes a
 * globally distributed, eventually consistent cache safe to put in front of
 * secrets — a stale entry is never read rather than being wrong.
 */
const cacheKey = (envId: string, revision: number) => `env:${envId}:rev:${revision}`;

export async function revision(db: D1Database, envId: string): Promise<number> {
  const row = await one<{ revision: number }>(db, `SELECT revision FROM environments WHERE id=?`, envId);
  if (!row) throw notFound("environment not found");
  return row.revision;
}

export async function snapshot(env: Env, key: CryptoKey, envId: string): Promise<Snapshot> {
  const rev = await revision(env.DB, envId);

  const cached = await env.CACHE.get(cacheKey(envId, rev), "json");
  if (cached) return { values: cached as Record<string, string>, revision: rev };

  const rows = await all<{ key_name: string; ciphertext: unknown }>(
    env.DB,
    `SELECT s.key_name, v.ciphertext FROM secrets s
     JOIN secret_versions v ON v.id=s.current_version_id
     WHERE s.environment_id=? AND s.deleted_at IS NULL`,
    envId,
  );
  const values: Record<string, string> = {};
  for (const row of rows) values[row.key_name] = await open(key, toBytes(row.ciphertext));

  await env.CACHE.put(cacheKey(envId, rev), JSON.stringify(values), { expirationTtl: 3600 });
  return { values, revision: rev };
}

export async function readSnapshot(env: Env, key: CryptoKey, userId: string, envId: string): Promise<Snapshot> {
  await allow(env.DB, userId, envId, "read");
  return snapshot(env, key, envId);
}

/**
 * Compare-and-swap on revision. The conditional UPDATE is the whole guard: if
 * someone else wrote since the caller last pulled, zero rows change and the
 * caller is told to diff rather than silently clobbering them.
 */
export async function writeSnapshot(
  env: Env,
  key: CryptoKey,
  userId: string | null,
  envId: string,
  values: Record<string, string>,
  expected: number,
): Promise<number> {
  if (userId) await allow(env.DB, userId, envId, "write");

  const bumped = await changed(
    env.DB,
    `UPDATE environments SET revision=revision+1 WHERE id=? AND revision=?`,
    envId,
    expected,
  );
  if (bumped !== 1) throw conflict("stale_revision", "remote secrets changed; run envi diff or envi pull");
  const rev = expected + 1;

  const statements: D1PreparedStatement[] = [];
  for (const name of Object.keys(values).sort()) {
    statements.push(...(await upsertStatements(env.DB, key, userId, envId, name, values[name]!)));
  }
  if (statements.length) await env.DB.batch(statements);

  await env.CACHE.put(cacheKey(envId, rev), JSON.stringify(values), { expirationTtl: 3600 });
  return rev;
}

async function upsertStatements(
  db: D1Database,
  key: CryptoKey,
  userId: string | null,
  envId: string,
  name: string,
  value: string,
): Promise<D1PreparedStatement[]> {
  const t = now();
  const existing = await one<{ id: string; version: number }>(
    db,
    `SELECT s.id AS id, COALESCE(MAX(v.version_number),0) AS version
     FROM secrets s LEFT JOIN secret_versions v ON v.secret_id=s.id
     WHERE s.environment_id=? AND s.key_name=? GROUP BY s.id`,
    envId,
    name,
  );
  const secretId = existing?.id ?? uuid();
  const versionId = uuid();
  const versionNumber = (existing?.version ?? 0) + 1;
  const ciphertext = blob(await seal(key, value));

  const out: D1PreparedStatement[] = [];
  if (!existing) {
    out.push(
      db
        .prepare(`INSERT INTO secrets(id,environment_id,key_name,created_at) VALUES(?,?,?,?)`)
        .bind(secretId, envId, name, t),
    );
  } else {
    out.push(db.prepare(`UPDATE secrets SET deleted_at=NULL WHERE id=?`).bind(secretId));
  }
  out.push(
    db
      .prepare(
        `INSERT INTO secret_versions(id,secret_id,ciphertext,nonce,version_number,created_by,created_at)
         VALUES(?,?,?,?,?,?,?)`,
      )
      // nonce stays empty to match the Go server, which prepends it to ciphertext.
      .bind(versionId, secretId, ciphertext, blob(new Uint8Array()), versionNumber, userId, t),
  );
  out.push(db.prepare(`UPDATE secrets SET current_version_id=? WHERE id=?`).bind(versionId, secretId));
  return out;
}

export async function remove(env: Env, userId: string, envId: string, name: string): Promise<void> {
  await allow(env.DB, userId, envId, "write");
  const count = await changed(
    env.DB,
    `UPDATE secrets SET deleted_at=? WHERE environment_id=? AND key_name=? AND deleted_at IS NULL`,
    now(),
    envId,
    name,
  );
  if (count !== 1) throw notFound("secret not found");
  await run(env.DB, `UPDATE environments SET revision=revision+1 WHERE id=?`, envId);
}

export async function serviceSnapshot(env: Env, key: CryptoKey, envId: string): Promise<Snapshot> {
  return snapshot(env, key, envId);
}
