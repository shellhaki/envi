export const now = () => Date.now();
export const uuid = () => crypto.randomUUID();

/** D1 returns BLOBs as number[]; queries compare against Uint8Array. */
export function blob(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

export function toBytes(value: unknown): Uint8Array {
  if (value instanceof Uint8Array) return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (Array.isArray(value)) return new Uint8Array(value);
  throw new Error("expected a blob");
}

export async function one<T>(db: D1Database, sql: string, ...binds: unknown[]): Promise<T | null> {
  return db.prepare(sql).bind(...binds).first<T>();
}

export async function all<T>(db: D1Database, sql: string, ...binds: unknown[]): Promise<T[]> {
  const { results } = await db.prepare(sql).bind(...binds).all<T>();
  return results ?? [];
}

export async function run(db: D1Database, sql: string, ...binds: unknown[]) {
  return db.prepare(sql).bind(...binds).run();
}

/** Rows actually written — the atomic primitive standing in for FOR UPDATE. */
export async function changed(db: D1Database, sql: string, ...binds: unknown[]): Promise<number> {
  const { meta } = await run(db, sql, ...binds);
  return meta.changes ?? 0;
}
