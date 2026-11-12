// Db is the only database interface the rest of the code sees. Both pg (Bun) and
// @neondatabase/serverless (Workers) provide a Pool with the same API underneath.

export type Row = Record<string, any>;

export interface Queryable {
  query<T extends Row = Row>(sql: string, params?: unknown[]): Promise<T[]>;
  one<T extends Row = Row>(sql: string, params?: unknown[]): Promise<T | undefined>;
  // exec runs a statement and returns how many rows it changed.
  exec(sql: string, params?: unknown[]): Promise<number>;
}

export interface Db extends Queryable {
  transaction<T>(work: (tx: Queryable) => Promise<T>): Promise<T>;
  // withClient runs work on one connection, for session-level locks.
  withClient<T>(work: (client: Queryable) => Promise<T>): Promise<T>;
  end(): Promise<void>;
}

type PgResult = { rows: any[]; rowCount: number | null };
type PgClient = { query(sql: string, params?: unknown[]): Promise<PgResult>; release(): void };
export type PgPool = {
  query(sql: string, params?: unknown[]): Promise<PgResult>;
  connect(): Promise<PgClient>;
  end(): Promise<void>;
};

function wrap(runner: { query(sql: string, params?: unknown[]): Promise<PgResult> }): Queryable {
  return {
    async query(sql, params) {
      return (await runner.query(sql, params)).rows;
    },
    async one(sql, params) {
      return (await runner.query(sql, params)).rows[0];
    },
    async exec(sql, params) {
      return (await runner.query(sql, params)).rowCount ?? 0;
    },
  };
}

export function makeDb(pool: PgPool): Db {
  return {
    ...wrap(pool),
    async transaction(work) {
      const client = await pool.connect();
      try {
        await client.query("BEGIN");
        const result = await work(wrap(client));
        await client.query("COMMIT");
        return result;
      } catch (err) {
        await client.query("ROLLBACK").catch(() => {});
        throw err;
      } finally {
        client.release();
      }
    },
    async withClient(work) {
      const client = await pool.connect();
      try {
        return await work(wrap(client));
      } finally {
        client.release();
      }
    },
    end: () => pool.end(),
  };
}

type TypeParsers = { setTypeParser(oid: number, parse: (value: string) => unknown): void };

// useTypes makes counts come back as numbers and dates as plain "YYYY-MM-DD" strings.
export function useTypes(types: TypeParsers) {
  types.setTypeParser(20, (v) => Number(v)); // int8
  types.setTypeParser(1700, (v) => Number(v)); // numeric
  types.setTypeParser(1082, (v) => v); // date
}

// isUniqueViolation and isInvalidText recognise two Postgres errors the API maps to replies.
export const isUniqueViolation = (err: unknown) => (err as { code?: string })?.code === "23505";
export const isInvalidText = (err: unknown) => (err as { code?: string })?.code === "22P02";
