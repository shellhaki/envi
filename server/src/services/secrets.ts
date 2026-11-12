// Every value is stored encrypted. Each change adds a row to secret_versions and bumps the
// environment's revision, which is how two people writing at once are detected.

import type { Cipher } from "../crypto";
import type { Db, Queryable } from "../db";
import { Conflict, NotFound } from "../errors";
import { allow } from "./access";

export type Snapshot = { values: Record<string, string>; revision: number };

// snapshot returns all values and the revision, read together, and records one audit event.
export async function snapshot(db: Db, cipher: Cipher, userId: string, envId: string): Promise<Snapshot> {
  await allow(db, userId, envId, "read");
  return db.transaction(async (tx) => {
    const env = await tx.one(`SELECT revision FROM environments WHERE id = $1 FOR SHARE`, [envId]);
    const values = await readValues(tx, cipher, envId);
    await tx.exec(
      `INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id, metadata)
       SELECT p.org_id, $1, 'secret.read', 'environment', $2, jsonb_build_object('secrets', $3::int)
         FROM environments e JOIN projects p ON p.id = e.project_id
        WHERE e.id = $2`,
      [userId, envId, Object.keys(values).length],
    );
    return { values, revision: Number(env!.revision) };
  });
}

// readForService is a read by a service token, which is already bound to this environment.
export async function readForService(db: Db, cipher: Cipher, serviceId: string, envId: string): Promise<Record<string, string>> {
  const values = await readValues(db, cipher, envId);
  await db
    .exec(
      `INSERT INTO audit_events (org_id, action, target_type, target_id, metadata)
       SELECT p.org_id, 'secret.read', 'service_identity', $1, jsonb_build_object('environment_id', $2::text)
         FROM environments e JOIN projects p ON p.id = e.project_id
        WHERE e.id = $2`,
      [serviceId, envId],
    )
    .catch(() => {});
  return values;
}

export async function revision(db: Db, envId: string): Promise<number> {
  const row = await db.one(`SELECT revision FROM environments WHERE id = $1`, [envId]);
  if (!row) throw new NotFound("environment not found");
  return Number(row.revision);
}

// put sets one secret. An empty userId means a service token, which was checked by the route.
export async function put(db: Db, cipher: Cipher, userId: string, envId: string, key: string, value: string): Promise<void> {
  if (userId) await allow(db, userId, envId, "write");
  await db.transaction(async (tx) => {
    await tx.exec(`UPDATE environments SET revision = revision + 1 WHERE id = $1`, [envId]);
    await writeVersion(tx, cipher, userId, envId, key, value);
  });
}

// putAll writes many secrets at once, only if the revision is still `expected`. Otherwise Conflict.
export async function putAll(
  db: Db, cipher: Cipher, userId: string, envId: string, values: Record<string, string>, expected: number,
): Promise<number> {
  if (userId) await allow(db, userId, envId, "write");
  return db.transaction(async (tx) => {
    const row = await tx.one(
      `UPDATE environments SET revision = revision + 1 WHERE id = $1 AND revision = $2 RETURNING revision`,
      [envId, expected],
    );
    if (!row) throw new Conflict("stale secret revision");
    for (const key of Object.keys(values).sort()) {
      await writeVersion(tx, cipher, userId, envId, key, values[key]);
    }
    return Number(row.revision);
  });
}

// remove marks one secret deleted. Its versions are kept.
export async function remove(db: Db, userId: string, envId: string, key: string): Promise<void> {
  await allow(db, userId, envId, "write");
  await db.transaction(async (tx) => {
    const secret = await tx.one(
      `UPDATE secrets SET deleted_at = now()
        WHERE environment_id = $1 AND key_name = $2 AND deleted_at IS NULL
       RETURNING id`,
      [envId, key],
    );
    if (!secret) throw new NotFound("secret not found");
    await tx.exec(
      `INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id)
       SELECT p.org_id, $1, 'secret.delete', 'secret', $2
         FROM environments e JOIN projects p ON p.id = e.project_id
        WHERE e.id = $3`,
      [userId, secret.id, envId],
    );
  });
}

// writeVersion encrypts a value, stores it as the secret's next version, and records an audit event.
async function writeVersion(tx: Queryable, cipher: Cipher, userId: string, envId: string, key: string, value: string) {
  const sealed = await cipher.seal(value);
  const secret = await tx.one(
    `INSERT INTO secrets (environment_id, key_name) VALUES ($1, $2)
         ON CONFLICT (environment_id, key_name) DO UPDATE SET deleted_at = NULL
     RETURNING id`,
    [envId, key],
  );
  const version = await tx.one(
    `INSERT INTO secret_versions (secret_id, ciphertext, nonce, version_number, created_by)
     VALUES ($1, $2, $3, (SELECT COALESCE(MAX(version_number), 0) + 1 FROM secret_versions WHERE secret_id = $1), $4)
     RETURNING id`,
    [secret!.id, sealed, Buffer.alloc(0), userId || null],
  );
  await tx.exec(`UPDATE secrets SET current_version_id = $1 WHERE id = $2`, [version!.id, secret!.id]);
  await tx.exec(
    `INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id)
     SELECT p.org_id, $1, 'secret.write', 'secret', $2
       FROM environments e JOIN projects p ON p.id = e.project_id
      WHERE e.id = $3`,
    [userId || null, secret!.id, envId],
  );
}

// readValues decrypts every current secret in an environment.
async function readValues(db: Queryable, cipher: Cipher, envId: string): Promise<Record<string, string>> {
  const rows = await db.query(
    `SELECT s.key_name, v.ciphertext
       FROM secrets s
       JOIN secret_versions v ON v.id = s.current_version_id
      WHERE s.environment_id = $1 AND s.deleted_at IS NULL`,
    [envId],
  );
  const values: Record<string, string> = {};
  for (const row of rows) values[row.key_name] = await cipher.open(row.ciphertext);
  return values;
}
