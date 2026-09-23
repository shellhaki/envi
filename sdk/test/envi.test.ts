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
  const calls: Array<{ url: string; method: string; auth?: string; body?: any }> = [];
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
    return new Response(reply.status === 204 ? null : JSON.stringify(reply.body ?? {}), {
      status: reply.status ?? 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof globalThis.fetch;
  return { fetchImpl, calls };
}

const store = { project: "acme-api", values: { THEME: "dark", REGION: "eu" } };

describe("reading", () => {
  test("a service token needs no project named", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: store }]);
    const envi = new Envi({ token: "st_abc", baseUrl: "https://api.test", fetch: fetchImpl });

    expect(await envi.get("THEME")).toBe("dark");
    expect(calls[0]!.url).toBe("https://api.test/kv");
    expect(calls[0]!.auth).toBe("Bearer st_abc");
    expect(envi.project).toBe("acme-api");
  });

  test("a missing key is undefined, and require() names it", async () => {
    const { fetchImpl } = stubFetch([{ body: store }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });

    expect(await envi.get("NOPE")).toBeUndefined();
    expect(envi.require("NOPE")).rejects.toThrow(EnviMissingKeyError);
    expect(await envi.require("THEME")).toBe("dark");
  });

  test("values load once however many reads happen", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: store }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });

    await Promise.all([envi.get("THEME"), envi.get("REGION"), envi.all()]);
    await envi.get("THEME");
    expect(calls.filter((c) => c.method === "GET")).toHaveLength(1);
  });

  test("all() and keys() report everything", async () => {
    const { fetchImpl } = stubFetch([{ body: store }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });

    expect(await envi.all()).toEqual({ THEME: "dark", REGION: "eu" });
    expect(await envi.keys()).toEqual(["REGION", "THEME"]);
  });

  test("refresh() picks up a change made elsewhere", async () => {
    const { fetchImpl } = stubFetch([
      { body: store },
      { body: { project: "acme-api", values: { THEME: "light" } } },
    ]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });

    expect(await envi.get("THEME")).toBe("dark");
    await envi.refresh();
    expect(await envi.get("THEME")).toBe("light");
  });
});

describe("writing", () => {
  test("set() sends the value and updates what's held", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: store }, { body: { key: "THEME" } }]);
    const envi = new Envi({ token: "st_abc", baseUrl: "https://api.test", fetch: fetchImpl });

    await envi.set("THEME", "light");
    const put = calls.find((c) => c.method === "PUT")!;
    expect(put.url).toBe("https://api.test/kv");
    expect(put.body).toEqual({ project: undefined, key: "THEME", value: "light" });
    // No further fetch: the new value is already known.
    expect(await envi.get("THEME")).toBe("light");
    expect(calls.filter((c) => c.method === "GET")).toHaveLength(1);
  });

  test("delete() removes it locally too", async () => {
    const { fetchImpl, calls } = stubFetch([{ body: store }, { status: 204 }]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });

    await envi.delete("THEME");
    expect(calls.find((c) => c.method === "DELETE")!.body).toEqual({ project: undefined, key: "THEME" });
    expect(await envi.get("THEME")).toBeUndefined();
    expect(await envi.keys()).toEqual(["REGION"]);
  });

  test("a read-only credential surfaces the server's refusal", async () => {
    const { fetchImpl } = stubFetch([
      { body: store },
      { status: 403, body: { error: "this credential may only read" } },
    ]);
    const envi = new Envi({ token: "st_abc", fetch: fetchImpl });
    expect(envi.set("THEME", "light")).rejects.toThrow(/may only read/);
  });
});

describe("api keys", () => {
  test("are exchanged for a session, then used as the bearer", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { body: store },
    ]);
    const envi = new Envi({
      apiKey: "envi_key",
      project: "acme-api",
      baseUrl: "https://api.test",
      fetch: fetchImpl,
    });

    expect(await envi.get("THEME")).toBe("dark");
    expect(calls[0]!.url).toBe("https://api.test/auth/api-key");
    expect(calls[0]!.body).toEqual({ key: "envi_key" });
    expect(calls[1]!.url).toBe("https://api.test/kv?project=acme-api");
    expect(calls[1]!.auth).toBe("Bearer access-1");
  });

  test("a 401 renews the session and retries once", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { status: 401, body: { error: "expired" } },
      { body: { access_token: "access-2", refresh_token: "refresh-2" } },
      { body: store },
    ]);
    const envi = new Envi({ apiKey: "envi_key", project: "acme-api", fetch: fetchImpl });

    expect(await envi.get("THEME")).toBe("dark");
    expect(calls.at(-1)!.auth).toBe("Bearer access-2");
  });

  // Refresh tokens are single-use, so two renewals at once would race and one
  // would be told its good session had expired.
  test("concurrent reads exchange the key only once", async () => {
    const { fetchImpl, calls } = stubFetch([
      { body: { access_token: "access-1", refresh_token: "refresh-1" } },
      { body: store },
    ]);
    const envi = new Envi({ apiKey: "envi_key", project: "acme-api", fetch: fetchImpl });

    await Promise.all([envi.get("THEME"), envi.get("REGION"), envi.keys()]);
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

  test("credentials must be exactly one, and a key needs a project", () => {
    expect(() => new Envi({})).toThrow(EnviAuthError);
    expect(() => new Envi({ token: "a", apiKey: "b" })).toThrow(EnviAuthError);
    expect(() => new Envi({ apiKey: "b" })).toThrow(/project/);
  });

  test("each failure maps to its own error, carrying the server's words", async () => {
    const bad = stubFetch([{ status: 401, body: { error: "invalid api key" } }]);
    expect(new Envi({ token: "bad", fetch: bad.fetchImpl }).get("X")).rejects.toThrow(EnviAuthError);

    const missing = stubFetch([
      { status: 404, body: { error: 'no project named "typo"; this account can see: acme-api' } },
    ]);
    expect(new Envi({ token: "st", fetch: missing.fetchImpl }).get("X")).rejects.toThrow(EnviNotFoundError);

    const broken = (async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof globalThis.fetch;
    expect(new Envi({ token: "st", fetch: broken }).get("X")).rejects.toThrow(EnviUnreachableError);
  });
});
