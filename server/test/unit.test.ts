import { afterEach, describe, expect, test } from "bun:test";
import { Hono } from "hono";
import { loadConfig, readConfig, setting } from "../src/config";
import { hashToken, makeCipher, randomBelow, toHex } from "../src/crypto";
import type { AppEnv } from "../src/deps";
import { readBody } from "../src/http";
import { memoryRedis } from "../src/redis";
import { bachsClient, money, signWebhook, verifyWebhook } from "../src/services/bachs";
import { ENCRYPTION_KEY } from "./helpers";

describe("encryption matches the Go server", () => {
  // Sealed and hashed by the old Go server with the same key, so data it wrote still reads.
  const sealedByGo = "0a0f1c2a64deb2bbac96ddd0cb8fe11b4a66b3a79bc11e3184b346b8d6e3bc24a4002df7a70ffd489c5505c84ad9c22aa6bec1c4a7f623";
  const goHashOfToken123 = "034192845dc489deca291f9f5ae0bb8e5472c991020bf64b3ebc6dec5a1d7e47";

  test("opens a value Go sealed", async () => {
    const cipher = await makeCipher(ENCRYPTION_KEY);
    expect(await cipher.open(Buffer.from(sealedByGo, "hex"))).toBe("sealed by go: ünïcode ✓");
  });

  test("seals in Go's layout: 12-byte nonce, ciphertext, 16-byte tag", async () => {
    const cipher = await makeCipher(ENCRYPTION_KEY);
    const sealed = await cipher.seal("abc");
    expect(sealed.length).toBe(12 + 3 + 16);
    expect(await cipher.open(sealed)).toBe("abc");
    expect(toHex(await cipher.seal("abc"))).not.toBe(toHex(sealed));
  });

  test("refuses tampered or short values and a wrong key", async () => {
    const cipher = await makeCipher(ENCRYPTION_KEY);
    const sealed = Buffer.from(sealedByGo, "hex");
    sealed[20] ^= 1;
    await expect(cipher.open(sealed)).rejects.toThrow();
    await expect(cipher.open(Buffer.alloc(5))).rejects.toThrow();
    const other = await makeCipher("ffffffffffffffffffffffffffffffff");
    await expect(other.open(Buffer.from(sealedByGo, "hex"))).rejects.toThrow();
    await expect(makeCipher("short")).rejects.toThrow();
  });

  test("token hashes match", async () => {
    expect(toHex(await hashToken("token-123"))).toBe(goHashOfToken123);
  });

  test("randomBelow stays in range", () => {
    for (let i = 0; i < 1000; i++) {
      const n = randomBelow(1_000_000);
      expect(n >= 0 && n < 1_000_000 && Number.isInteger(n)).toBe(true);
    }
  });
});

describe("bachs", () => {
  const body = `{"id":"evt_1","type":"collection.succeeded","created_at":"2026-10-15T10:00:00Z","data":{"charge_id":"ch_1"}}`;
  const now = new Date();
  const headers = (entries: Record<string, string>) => new Headers(entries);

  test("money writes minor units as decimals", () => {
    const cases: Record<number, string> = { 0: "0.00", 40: "0.40", 400: "4.00", 60000: "600.00", 7: "0.07", 123456: "1234.56" };
    for (const [minor, text] of Object.entries(cases)) expect(money(Number(minor))).toBe(text);
  });

  test("a good signature verifies", async () => {
    const event = await verifyWebhook("whsec_x", headers({ "X-Bachs-Signature-V2": await signWebhook("whsec_x", body, now) }), body, now);
    expect(event).toMatchObject({ id: "evt_1", type: "collection.succeeded" });
  });

  test("forged and replayed deliveries are refused", async () => {
    const good = await signWebhook("whsec_x", body, now);
    const later = new Date(now.getTime() + 6 * 60 * 1000);
    const cases: [Headers, string, Date][] = [
      [headers({ "X-Bachs-Signature-V2": await signWebhook("whsec_other", body, now) }), body, now],
      [headers({ "X-Bachs-Signature-V2": good }), body.replace("ch_1", "ch_2"), now],
      [headers({ "X-Bachs-Signature-V2": good }), body, later],
      [headers({ "X-Bachs-Signature-V2": await signWebhook("whsec_x", body, new Date(now.getTime() + 10 * 60 * 1000)) }), body, now],
      [headers({}), body, now],
      [headers({ "X-Bachs-Signature-V2": "nonsense" }), body, now],
      [headers({ "X-Bachs-Signature-V2": "v1=abcd" }), body, now],
    ];
    for (const [h, b, at] of cases) await expect(verifyWebhook("whsec_x", h, b, at)).rejects.toThrow();
    await expect(verifyWebhook("", headers({ "X-Bachs-Signature-V2": good }), body, now)).rejects.toThrow();
  });

  test("either secret verifies during a rotation", async () => {
    const oldSig = await signWebhook("whsec_old", body, now);
    const newSig = await signWebhook("whsec_new", body, now);
    const both = `${oldSig},${newSig.slice(newSig.indexOf("v1="))}`;
    for (const secret of ["whsec_old", "whsec_new"]) {
      expect(await verifyWebhook(secret, headers({ "X-Bachs-Signature-V2": both }), body, now)).toBeDefined();
    }
  });

  test("the older two-header form verifies", async () => {
    const [t, v1] = (await signWebhook("whsec_x", body, now)).split(",");
    const h = headers({ "X-Bachs-Timestamp": t.slice(2), "X-Bachs-Signature": v1.slice(3) });
    expect(await verifyWebhook("whsec_x", h, body, now)).toBeDefined();
  });

  test("the key picks the server", () => {
    expect(bachsClient("sk_sandbox_abc").live).toBe(false);
    expect(bachsClient("sk_live_abc").live).toBe(true);
    for (const bad of ["", "pk_live_abc", "sk_test_abc", "abc"]) expect(() => bachsClient(bad)).toThrow();
  });

  describe("requests", () => {
    const realFetch = globalThis.fetch;
    afterEach(() => {
      globalThis.fetch = realFetch;
    });

    test("a charge carries the key, the idempotency key and a decimal amount", async () => {
      let seen: Request | undefined;
      globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
        seen = new Request(input, init);
        return new Response(`{"charge_id":"ch_9","status":"processing"}`, { status: 201 });
      }) as typeof fetch;

      const charge = await bachsClient("sk_sandbox_k").chargeSavedCard("cust_1", 1200, "USD", "Envi", "payg:org:2026-09:1");
      expect(charge).toEqual({ id: "ch_9", status: "processing" });
      expect(seen!.url).toBe("https://sandbox-api.bachs.io/v1/charges");
      expect(seen!.headers.get("Authorization")).toBe("Bearer sk_sandbox_k");
      expect(seen!.headers.get("Idempotency-Key")).toBe("payg:org:2026-09:1");
      expect(await seen!.json()).toMatchObject({ amount: "12.00", currency: "USD", customer: "cust_1", reference: "payg:org:2026-09:1" });
    });

    test("an API error surfaces with its status", async () => {
      globalThis.fetch = (async () => new Response(`{"error":"card declined"}`, { status: 402 })) as unknown as typeof fetch;
      await expect(bachsClient("sk_sandbox_k").chargeSavedCard("c", 100, "USD", "x", "k")).rejects.toMatchObject({ status: 402 });
    });
  });
});

describe("config", () => {
  const complete = {
    DATABASE_URL: "postgres://db",
    UPSTASH_REDIS_REST_URL: "https://redis",
    UPSTASH_REDIS_REST_TOKEN: "t",
    RESEND_API_KEY: "re_x",
    RESEND_FROM: "Envi <a@b.co>",
    ENVI_ENCRYPTION_KEY: ENCRYPTION_KEY,
  };

  test("defaults", () => {
    const c = loadConfig({});
    expect(c.webUrl).toBe("http://localhost:3000");
    expect(c.port).toBe(8080);
    expect(c.mode).toBe("self-hosted");
    expect(c.currency).toBe("NGN");
  });

  test("quotes and spaces are stripped", () => {
    expect(setting({ X: ` "value" ` }, "X")).toBe("value");
    expect(setting({ X: `'value'` }, "X")).toBe("value");
    expect(setting({ X: `"value'` }, "X")).toBe(`"value'`);
    expect(loadConfig({ ENVI_ADMINS: " A@Example.com , ,b@example.com" }).admins).toEqual(["a@example.com", "b@example.com"]);
    expect(loadConfig({ ENVI_MODE: '"Hosted"' }).mode).toBe("hosted");
    expect(loadConfig({ ENVI_MODE: "nonsense" }).mode).toBe("self-hosted");
  });

  test("readConfig names the first missing setting", () => {
    expect(readConfig(complete).databaseUrl).toBe("postgres://db");
    expect(readConfig({ ...complete, UPSTASH_REDIS_REST_URL: "", REDIS_URL: "redis://x" }).redisUrl).toBe("redis://x");
    expect(() => readConfig({ ...complete, DATABASE_URL: "" })).toThrow("DATABASE_URL is required");
    expect(() => readConfig({ ...complete, UPSTASH_REDIS_REST_TOKEN: "" })).toThrow("REDIS_URL");
    expect(() => readConfig({ ...complete, RESEND_FROM: "" })).toThrow("RESEND_API_KEY and RESEND_FROM are required");
    expect(() => readConfig({ ...complete, ENVI_ENCRYPTION_KEY: "short" })).toThrow("exactly 32 bytes");
  });

  test("a deployed server needs a public web URL", () => {
    expect(() => readConfig({ ...complete, ENVIRONMENT: "production" })).toThrow("ENVI_WEB_URL");
    expect(() => readConfig({ ...complete, ENVIRONMENT: "production", ENVI_WEB_URL: "http://127.0.0.1:3000" })).toThrow("ENVI_WEB_URL");
    expect(readConfig({ ...complete, ENVIRONMENT: "production", ENVI_WEB_URL: "https://envisecrets.com" }).webUrl).toBe("https://envisecrets.com");
  });
});

describe("readBody", () => {
  const app = new Hono<AppEnv>().post("/", async (c) =>
    c.json({ body: await readBody(c, { name: "string", n: "number", ttl: "number?", on: "boolean", map: "strings" }) }),
  );
  const read = async (body: string) => ((await (await app.request("/", { method: "POST", body })).json()) as { body: unknown }).body;

  test("missing fields get defaults", async () => {
    expect(await read("{}")).toEqual({ name: "", n: 0, ttl: null, on: false, map: null });
    expect(await read(`{"name":"x","n":3,"ttl":0,"on":true,"map":{"A":"1"},"extra":1}`)).toEqual({ name: "x", n: 3, ttl: 0, on: true, map: { A: "1" } });
  });

  test("wrong types and non-objects are null", async () => {
    for (const body of ["", "nope", "[]", "null", `{"name":1}`, `{"n":"1"}`, `{"n":1.5}`, `{"on":"yes"}`, `{"map":{"A":1}}`]) {
      expect(await read(body)).toBeNull();
    }
  });
});

describe("memory redis", () => {
  test("counters and values expire", async () => {
    const redis = memoryRedis();
    expect(await redis.incr("n", 60)).toBe(1);
    expect(await redis.incr("n", 60)).toBe(2);
    await redis.set("k", "v", 60);
    expect(await redis.get("k")).toBe("v");
    await redis.del("k");
    expect(await redis.get("k")).toBeNull();
    await redis.set("gone", "v", 0);
    expect(await redis.get("gone")).toBeNull();
  });
});
