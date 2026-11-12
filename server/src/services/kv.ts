// A project-wide key-value store that applications read and write over HTTP.
// Unlike secrets, entries belong to the whole project and keep no history. Values are encrypted.

import type { Cipher } from "../crypto";
import type { Db } from "../db";
import { Invalid } from "../errors";

export const MAX_VALUE_BYTES = 64 * 1024;

export async function allEntries(db: Db, cipher: Cipher, projectId: string): Promise<Record<string, string>> {
  const rows = await db.query(
    `SELECT key_name, ciphertext FROM kv_entries WHERE project_id = $1 ORDER BY key_name`,
    [projectId],
  );
  const values: Record<string, string> = {};
  for (const row of rows) values[row.key_name] = await cipher.open(row.ciphertext);
  return values;
}

// setEntry writes a value, replacing any previous one.
export async function setEntry(db: Db, cipher: Cipher, projectId: string, rawKey: string, value: string): Promise<void> {
  const key = rawKey.trim();
  validateKey(key);
  if (new TextEncoder().encode(value).length > MAX_VALUE_BYTES) throw new Invalid("value is too large; the limit is 64KB");
  await db.exec(
    `INSERT INTO kv_entries (project_id, key_name, ciphertext) VALUES ($1, $2, $3)
         ON CONFLICT (project_id, key_name) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = now()`,
    [projectId, key, await cipher.seal(value)],
  );
}

// deleteEntry removes an entry. Deleting a missing entry is not an error.
export async function deleteEntry(db: Db, projectId: string, key: string): Promise<void> {
  await db.exec(`DELETE FROM kv_entries WHERE project_id = $1 AND key_name = $2`, [projectId, key]);
}

function validateKey(key: string) {
  if (key === "") throw new Invalid("a key is required");
  if (new TextEncoder().encode(key).length > 255) throw new Invalid("key is too long; the limit is 255 characters");
  if (/[\0\n\r]/.test(key)) throw new Invalid("a key cannot contain newlines or null bytes");
}
