import { afterEach, describe, expect, test } from "bun:test";
import { createHmac } from "node:crypto";
import { accessExpired, createSession, readSession } from "./web-session";

const original = process.env.ENVI_SESSION_SECRET;
afterEach(() => { process.env.ENVI_SESSION_SECRET = original; });

const secret = "01234567890123456789012345678901";

// Mirrors the module's wire format so a legacy payload can be forged in a test.
function sign(key: string, payload: object) {
  const encode = (value: object) => Buffer.from(JSON.stringify(value)).toString("base64url");
  const body = `${encode({ alg: "HS256", typ: "JWT" })}.${encode(payload)}`;
  return `${body}.${createHmac("sha256", key).update(body).digest("base64url")}`;
}

describe("web session", () => {
  test("signs and reads tokens", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const session = readSession(createSession("access", "refresh", 900));
    expect(session?.access).toBe("access");
    expect(session?.refresh).toBe("refresh");
  });

  test("rejects tampering", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const value = createSession("access", "refresh", 900);
    expect(readSession(`${value.slice(0, -1)}x`)).toBeNull();
  });

  test("tracks the access token expiry separately from the cookie", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const session = readSession(createSession("access", "refresh", 900))!;
    expect(accessExpired(session)).toBe(false);
    // The cookie outlives the access token, which is the whole point: the
    // refresh token stays readable after the access token is due.
    expect(session.exp).toBeGreaterThan(session.aexp);
  });

  test("reports a due access token as expired", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const session = readSession(createSession("access", "refresh", 1))!;
    expect(accessExpired(session)).toBe(true);
  });

  test("falls back to a future expiry when expires_in is missing", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    // A missing expires_in must not produce an already-due session, or Proxy
    // would redirect to the refresh handler in a loop.
    const session = readSession(createSession("access", "refresh"))!;
    expect(accessExpired(session)).toBe(false);
  });

  test("treats a non-numeric aexp as due for refresh", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const session = readSession(createSession("access", "refresh", 900))!;
    expect(accessExpired({ ...session, aexp: undefined as unknown as number })).toBe(true);
  });

  test("accepts a cookie written before aexp existed and refreshes it", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    // Sessions issued by the previous format carried no aexp. They must stay
    // readable — otherwise deploying this signs every active user out — but must
    // report as due so the next request rotates them.
    const legacy = sign(secret, { access: "access", refresh: "refresh", exp: Math.floor(Date.now() / 1000) + 2592000 });
    const session = readSession(legacy);
    expect(session?.access).toBe("access");
    expect(accessExpired(session!)).toBe(true);
  });

  test("rejects a session signed with a different secret", () => {
    process.env.ENVI_SESSION_SECRET = secret;
    const value = createSession("access", "refresh", 900);
    process.env.ENVI_SESSION_SECRET = "abcdefghijabcdefghijabcdefghij12";
    expect(readSession(value)).toBeNull();
  });
});
