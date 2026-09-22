import { describe, expect, test } from "bun:test";
import {
  Envi,
  EnviAuthError,
  EnviMissingKeyError,
  EnviNotFoundError,
  EnviUnreachableError,
} from "../src/index.js";

/** A fetch stand-in that records calls and replies from a script. */
function stubFetch(replies: Array<{ status?: number; body?: unknown }>) {
  const calls: Array<{ url: string; method: string; auth?: string; body?: unknown }> = [];
  let i = 0;
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    const headers = (init?.headers ?? {}) as Record<string, string>;
    calls.push({
      url: String(url),
      method: init?.method ?? "GET",
      auth: headers["Authorization"],
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    const reply = replies[Math.min(i++, replies.length - 1)]!;
    return new Response(JSON.stringify(reply.body ?? {}), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof globalThis.fetch;
  return { fetchImpl, calls };
}

const values = { DATABASE_URL: "postgres://x", PORT: "3000" };
const snapshot = { project: "acme-api", environment: "production", values, revision: 7 };

describe("service token", () => {
  test("sends the token straight through and needs no names", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: snapshot }]);
    const envi = new Envi({ token: "st_abc", baseUrl: "https://api.test", fetch: fetchImpl });
    await envi.ready();

    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe("https://api.test/values");
    expect(calls[0]!.auth).toBe("Bearer st_abc");
    expect(envi.get("PORT")).toBe("3000");
    expect(envi.source).toEqual({ project: "acme-api", environment: "production", revision: 7 });
  });

  test("ready() fetches once however many times it is called", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: snapshot }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    await Promise.all([envi.ready(), envi.ready(), envi.ready()]);
    await envi.ready();
    expect(calls).toHaveLength(1);
  });

  test("refresh() re-reads", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: snapshot },
      { body: { ...snapshot, values: { PORT: "4000" }, revision: 8 } },
    ]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    await envi.ready();
    await envi.refresh();
    expect(calls).toHaveLength(2);
    expect(envi.get("PORT")).toBe("4000");
    expect(envi.source.revision).toBe(8);
  });
});

describe("api key", () => {
  test("is exchanged for a session, then used as the bearer", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { body: snapshot },
    ]);
    const envi = new Envi({
      apiKey: "envi_key",
      project: "acme-api",
      environment: "production",
      baseUrl: "https://api.test",
      fetch: fetchImpl,
    });
    await envi.ready();

    expect(calls[0]!.url).toBe("https://api.test/auth/api-key");
    expect(calls[0]!.body).toEqual({ key: "envi_key" });
    expect(calls[1]!.url).toBe("https://api.test/values?project=acme-api&environment=production");
    expect(calls[1]!.auth).toBe("Bearer access-1");
    expect(envi.get("DATABASE_URL")).toBe("postgres://x");
  });

  test("a 401 renews the session and retries once", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { status: 401, body: { error: "expired" } },
      { body: { access_token: "access-2", refresh_token: "refresh-2" } },
      { body: snapshot },
    ]);
    const envi = new Envi({ apiKey: "envi_key", project: "acme-api", fetch: fetchImpl });
    await envi.ready();

    expect(calls.map((c) => c.url.split("/").pop()!.split("?")[0])).toEqual([
      "api-key",
      "values",
      "refresh",
      "values",
    ]);
    expect(calls[3]!.auth).toBe("Bearer access-2");
    expect(envi.get("PORT")).toBe("3000");
  });

  // Refresh tokens are single-use, so two renewals at once would race and one
  // would be told its good session had expired.
  test("concurrent loads exchange the key only once", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { body: snapshot },
    ]);
    const envi = new Envi({ apiKey: "envi_key", project: "acme-api", fetch: fetchImpl });
    await Promise.all([envi.ready(), envi.ready(), envi.ready()]);
    expect(calls.filter((c) => c.url.includes("/auth/api-key"))).toHaveLength(1);
  });
});

describe("mistakes", () => {
  test("a browser refuses to construct one", () => {
    (globalThis as Record<string, unknown>)["window"] = {};
    try {
      expect(() => new Envi({ token: "st_abc" })).toThrow(/server-side only/);
    } finally {
      delete (globalThis as Record<string, unknown>)["window"];
    }
  });

  test("credentials must be exactly one", () => {
    expect(() => new Envi({})).toThrow(EnviAuthError);
    expect(() => new Envi({ token: "a", apiKey: "b" })).toThrow(EnviAuthError);
    expect(() => new Envi({ apiKey: "b" })).toThrow(/project/);
  });

  test("reading before ready() says so", () => {
    const { fetchImpl } = stubFetch([{ body: snapshot }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    expect(() => envi.get("PORT")).toThrow(/ready\(\)/);
  });

  test("require() names the key it wanted", async () => {
    const { fetchImpl } = stubFetch([{ body: snapshot }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    await envi.ready();
    expect(() => envi.require("NOPE")).toThrow(EnviMissingKeyError);
    expect(() => envi.require("NOPE")).toThrow(/NOPE/);
    expect(envi.require("PORT")).toBe("3000");
  });

  test("each failure maps to its own error, carrying the server's words", async () => {
    const unauthorized = stubFetch([{ status: 401, body: { error: "invalid api key" } }]);
    await expect(new Envi({ token: "bad", fetch: unauthorized.fetchImpl }).ready()).rejects.toThrow(
      EnviAuthError,
    );

    const missing = stubFetch([
      { status: 404, body: { error: 'no project named "typo"; this account can see: acme-api' } },
    ]);
    const notFound = new Envi({ token: "st", fetch: missing.fetchImpl }).ready();
    await expect(notFound).rejects.toThrow(EnviNotFoundError);
    await expect(notFound).rejects.toThrow(/acme-api/);

    const broken = (async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof globalThis.fetch;
    await expect(new Envi({ token: "st", fetch: broken }).ready()).rejects.toThrow(
      EnviUnreachableError,
    );
  });

  test("values cannot be changed through all()", async () => {
    const { fetchImpl } = stubFetch([{ body: snapshot }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    await envi.ready();
    const all = envi.all();
    expect(() => {
      (all as Record<string, string>)["PORT"] = "9999";
    }).toThrow();
    expect(envi.get("PORT")).toBe("3000");
  });
});
