import { describe, expect, test } from "bun:test";
import { call, newUser, testDeps } from "./helpers";

describe("login codes", () => {
  test("a code signs in and creates the account once", async () => {
    const deps = await testDeps();
    const email = `login-${Date.now()}@example.com`;

    expect((await call(deps, "/auth/request-otp", { method: "POST", body: { email } })).status).toBe(202);
    const code = deps.mailer.codeFor(email);
    expect(code).toHaveLength(6);

    const signedIn = await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code } });
    expect(signedIn.status).toBe(200);
    expect(signedIn.json.expires_in).toBe(900);

    const me = await call(deps, "/me", { token: signedIn.json.access_token });
    expect(me.status).toBe(200);
    expect(me.json.Email).toBe(email);
    expect(me.json.OrganizationID).toBeString();

    await call(deps, "/auth/request-otp", { method: "POST", body: { email: email.toUpperCase() } });
    const again = await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code: deps.mailer.codeFor(email.toUpperCase()) } });
    const meAgain = await call(deps, "/me", { token: again.json.access_token });
    expect(meAgain.json.ID).toBe(me.json.ID);
  });

  test("a code works only once", async () => {
    const deps = await testDeps();
    const email = `once-${Date.now()}@example.com`;
    await call(deps, "/auth/request-otp", { method: "POST", body: { email } });
    const code = deps.mailer.codeFor(email);
    expect((await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code } })).status).toBe(200);
    expect((await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code } })).status).toBe(401);
  });

  test("a wrong code is refused and guesses run out", async () => {
    const deps = await testDeps();
    const email = `guess-${Date.now()}@example.com`;
    await call(deps, "/auth/request-otp", { method: "POST", body: { email } });
    const code = deps.mailer.codeFor(email);
    const wrong = code === "000000" ? "111111" : "000000";
    for (let i = 0; i < 10; i++) {
      expect((await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code: wrong } })).status).toBe(401);
    }
    expect((await call(deps, "/auth/verify-otp", { method: "POST", body: { email, code } })).status).toBe(401);
  });

  test("requests are rate limited per email", async () => {
    const deps = await testDeps();
    const email = `limit-${Date.now()}@example.com`;
    for (let i = 0; i < 20; i++) await call(deps, "/auth/request-otp", { method: "POST", body: { email } });
    const limited = await call(deps, "/auth/request-otp", { method: "POST", body: { email } });
    expect(limited.status).toBe(429);
    expect(limited.json.code).toBe("rate_limited");
  });

  test("bad input is a 400", async () => {
    const deps = await testDeps();
    expect((await call(deps, "/auth/request-otp", { method: "POST", body: { email: "nope" } })).status).toBe(400);
    expect((await call(deps, "/auth/request-otp", { method: "POST", body: "not json" })).status).toBe(400);
    expect((await call(deps, "/auth/verify-otp", { method: "POST", body: { email: "a@b.co" } })).status).toBe(400);
  });
});

describe("sessions", () => {
  test("a refresh token is used up when refreshed", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const refreshed = await call(deps, "/auth/refresh", { method: "POST", body: { refresh_token: user.refresh } });
    expect(refreshed.status).toBe(200);
    expect(refreshed.json.refresh_token).not.toBe(user.refresh);
    expect((await call(deps, "/auth/refresh", { method: "POST", body: { refresh_token: user.refresh } })).status).toBe(401);
    expect((await call(deps, "/me", { token: refreshed.json.access_token })).status).toBe(200);
  });

  test("logout ends the session", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    expect((await call(deps, "/auth/logout", { method: "POST", body: { refresh_token: user.refresh } })).status).toBe(204);
    expect((await call(deps, "/auth/refresh", { method: "POST", body: { refresh_token: user.refresh } })).status).toBe(401);
  });

  test("signed-in routes need a valid token", async () => {
    const deps = await testDeps();
    expect((await call(deps, "/me")).status).toBe(401);
    expect((await call(deps, "/me", { token: "garbage" })).status).toBe(401);
    expect((await call(deps, "/projects", { token: "garbage" })).status).toBe(401);
  });
});

describe("device login", () => {
  test("the CLI gets tokens once the browser approves", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const start = await call(deps, "/auth/device/code", { method: "POST" });
    expect(start.status).toBe(200);
    expect(start.json.verification_uri).toBe("https://envi.test/device");
    expect(start.json.verification_uri_complete).toContain(encodeURIComponent(start.json.user_code));

    const pending = await call(deps, "/auth/device/token", { method: "POST", body: { device_code: start.json.device_code } });
    expect(pending.status).toBe(400);
    expect(pending.json.code).toBe("authorization_pending");

    const approve = await call(deps, "/auth/device/approve", { method: "POST", token: user.token, body: { user_code: start.json.user_code.toLowerCase() } });
    expect(approve.status).toBe(204);

    const done = await call(deps, "/auth/device/token", { method: "POST", body: { device_code: start.json.device_code } });
    expect(done.status).toBe(200);
    const me = await call(deps, "/me", { token: done.json.access_token });
    expect(me.json.ID).toBe(user.userId);

    const reused = await call(deps, "/auth/device/token", { method: "POST", body: { device_code: start.json.device_code } });
    expect(reused.status).toBe(400);
  });

  test("a denied code tells the CLI", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const start = await call(deps, "/auth/device/code", { method: "POST" });
    expect((await call(deps, "/auth/device/deny", { method: "POST", token: user.token, body: { user_code: start.json.user_code } })).status).toBe(204);
    const denied = await call(deps, "/auth/device/token", { method: "POST", body: { device_code: start.json.device_code } });
    expect(denied.json.code).toBe("access_denied");
  });

  test("an unknown code is refused", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    expect((await call(deps, "/auth/device/approve", { method: "POST", token: user.token, body: { user_code: "ZZZZ-ZZZZ" } })).status).toBe(400);
    expect((await call(deps, "/auth/device/token", { method: "POST", body: { device_code: "nope" } })).json.code).toBe("invalid_grant");
  });
});

describe("api keys", () => {
  test("a key signs in with its permission as a ceiling", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const created = await call(deps, "/me/api-keys", { method: "POST", token: user.token, body: { name: "ci", permission: "read" } });
    expect(created.status).toBe(201);
    expect(created.json.secret).toStartWith("envi_");
    expect(created.json.expires_at).toBeString();

    const listed = await call(deps, "/me/api-keys", { token: user.token });
    expect(listed.json).toHaveLength(1);
    expect(listed.json[0].secret).toBeUndefined();

    const session = await call(deps, "/auth/api-key", { method: "POST", body: { key: created.json.secret } });
    expect(session.status).toBe(200);
    const token = session.json.access_token;
    expect((await call(deps, "/projects", { token })).status).toBe(200);
    expect((await call(deps, "/projects", { method: "POST", token, body: { org_id: user.orgId, name: "x" } })).status).toBe(403);
    expect((await call(deps, "/me/api-keys", { method: "POST", token, body: { name: "escalate", permission: "manage" } })).status).toBe(403);

    expect((await call(deps, `/me/api-keys/${created.json.id}`, { method: "DELETE", token: user.token })).status).toBe(204);
    expect((await call(deps, "/auth/api-key", { method: "POST", body: { key: created.json.secret } })).status).toBe(401);
    expect((await call(deps, `/me/api-keys/${created.json.id}`, { method: "DELETE", token: user.token })).status).toBe(404);
  });

  test("ttl_seconds 0 never expires and bad permissions are refused", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const forever = await call(deps, "/me/api-keys", { method: "POST", token: user.token, body: { name: "forever", ttl_seconds: 0 } });
    expect(forever.json.expires_at).toBeNull();
    expect(forever.json.permission).toBe("read");
    expect((await call(deps, "/me/api-keys", { method: "POST", token: user.token, body: { name: "x", permission: "root" } })).status).toBe(400);
    expect((await call(deps, "/me/api-keys", { method: "POST", token: user.token, body: { name: " " } })).status).toBe(400);
  });
});
