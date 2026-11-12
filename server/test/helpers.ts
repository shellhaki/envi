import pg from "pg";
import { readFileSync } from "node:fs";
import { app } from "../src/app";
import { loadConfig, type Config } from "../src/config";
import { makeCipher } from "../src/crypto";
import { makeDb, useTypes, type Db } from "../src/db";
import type { Deps } from "../src/deps";
import type { Mailer, Message } from "../src/mail";
import { memoryRedis } from "../src/redis";
import type { Bachs } from "../src/services/bachs";
import { provision } from "../src/services/workspace";
import { startSession } from "../src/services/sessions";

// Tests run against a throwaway local database, created once per run from migrations/schema.sql.
// TEST_ADMIN_DATABASE_URL is any database on the server that may create and drop envi_bun_test.
const ADMIN_URL = process.env.TEST_ADMIN_DATABASE_URL ?? "postgres://localhost/postgres";
const TEST_DB = "envi_bun_test";
export const TEST_DATABASE_URL = Object.assign(new URL(ADMIN_URL), { pathname: `/${TEST_DB}` }).toString();
export const ENCRYPTION_KEY = "0123456789abcdef0123456789abcdef";

let ready: Promise<Db> | undefined;

async function createDatabase(): Promise<Db> {
  const admin = new pg.Client({ connectionString: ADMIN_URL });
  await admin.connect();
  await admin.query(`DROP DATABASE IF EXISTS ${TEST_DB} WITH (FORCE)`);
  await admin.query(`CREATE DATABASE ${TEST_DB}`);
  await admin.end();

  useTypes(pg.types);
  const pool = new pg.Pool({ connectionString: TEST_DATABASE_URL, max: 10 });
  await pool.query(readFileSync(new URL("../../migrations/schema.sql", import.meta.url), "utf8"));
  return makeDb(pool);
}

export function testDb(): Promise<Db> {
  ready ??= createDatabase();
  return ready;
}

export class FakeMailer implements Mailer {
  sent: Message[] = [];
  async send(message: Message) {
    this.sent.push(message);
  }
  async sendMany(messages: Message[]) {
    this.sent.push(...messages);
    return 0;
  }
  // codeFor returns the latest 6-digit login code emailed to an address.
  codeFor(email: string) {
    const message = this.sent.findLast((m) => m.to === email && m.subject === "Envi login code");
    return message?.text.match(/\d{6}/)?.[0] ?? "";
  }
}

export class FakeBachs implements Bachs {
  live = false;
  calls: string[] = [];
  chargeStatus = "processing";
  failCharges = false;
  async createCustomer(email: string) {
    this.calls.push(`customer ${email}`);
    return `cus_${email}`;
  }
  async saveCardCheckout(customerId: string) {
    this.calls.push(`save-card ${customerId}`);
    return { checkout_id: "chk_1", checkout_url: "https://pay.test/save-card" };
  }
  async subscriptionCheckout(customerId: string, productId: string) {
    this.calls.push(`subscribe ${customerId} ${productId}`);
    return { checkout_id: "chk_2", checkout_url: "https://pay.test/team" };
  }
  async cancelSubscription(subscriptionId: string) {
    this.calls.push(`cancel ${subscriptionId}`);
  }
  async chargeSavedCard(customerId: string, minor: number, currency: string, _description: string, key: string) {
    this.calls.push(`charge ${customerId} ${minor} ${currency} ${key}`);
    if (this.failCharges) throw new Error("card declined");
    return { id: `ch_${key}`, status: this.chargeStatus };
  }
  async portalUrl(customerId: string) {
    return `https://pay.test/portal/${customerId}`;
  }
}

// settle waits for work handed to deps.background, like the welcome email.
export type TestDeps = Deps & { mailer: FakeMailer; settle: () => Promise<unknown> };

export async function testDeps(overrides: Partial<Config> = {}, bachs: Bachs | null = null): Promise<TestDeps> {
  const config = {
    ...loadConfig({ ENVI_WEB_URL: "https://envi.test", ENVI_ENCRYPTION_KEY: ENCRYPTION_KEY }),
    databaseUrl: TEST_DATABASE_URL,
    ...overrides,
  };
  const background: Promise<unknown>[] = [];
  return {
    config,
    db: await testDb(),
    redis: memoryRedis(),
    cipher: await makeCipher(ENCRYPTION_KEY),
    mailer: new FakeMailer(),
    bachs,
    users: null,
    background: (work) => void background.push(work),
    now: () => new Date(),
    settle: () => Promise.all(background),
  };
}

type RequestOptions = { method?: string; token?: string; body?: unknown; headers?: Record<string, string> };

export async function call(deps: Deps, path: string, options: RequestOptions = {}) {
  const headers: Record<string, string> = { ...options.headers };
  if (options.token) headers.Authorization = `Bearer ${options.token}`;
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  const response = await app.fetch(
    new Request(`http://api.test${path}`, {
      method: options.method ?? "GET",
      headers,
      body: options.body === undefined ? undefined : typeof options.body === "string" ? options.body : JSON.stringify(options.body),
    }),
    { deps },
  );
  const text = await response.text();
  let json: any;
  try {
    json = text ? JSON.parse(text) : undefined;
  } catch {
    json = undefined;
  }
  return { status: response.status, json, text };
}

let counter = 0;

// newUser creates an account and returns a signed-in access token for it.
export async function newUser(deps: Deps, label = "user") {
  const email = `${label}-${Date.now()}-${counter++}@example.com`;
  const account = await provision(deps, email);
  const tokens = await startSession(deps.db, account.userId);
  return { email, userId: account.userId, orgId: account.organizationId, token: tokens.access, refresh: tokens.refresh };
}

// newProject creates a project for a user and returns it with its default environment.
export async function newProject(deps: Deps, user: { token: string; orgId: string }, name = `project-${counter++}`) {
  const project = await call(deps, "/projects", { method: "POST", token: user.token, body: { org_id: user.orgId, name } });
  const envs = await call(deps, `/projects/${project.json.ID}/environments`, { token: user.token });
  return { project: project.json, env: envs.json[0] };
}
