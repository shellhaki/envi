import { describe, expect, test } from "bun:test";
import { call, newUser, testDeps } from "./helpers";

async function withAdmin() {
  const base = await testDeps();
  const admin = await newUser(base, "admin");
  const deps = { ...base, config: { ...base.config, admins: [admin.email] } };
  return { deps, admin };
}

describe("operator access", () => {
  test("everyone else gets a 404", async () => {
    const { deps } = await withAdmin();
    const user = await newUser(deps);
    for (const path of ["/admin/whoami", "/admin/stats", "/admin/operators", "/admin/announcements"]) {
      expect((await call(deps, path, { token: user.token })).status).toBe(404);
    }
    expect((await call(deps, "/admin/whoami")).status).toBe(401);
  });

  test("an ENVI_ADMINS address is an admin", async () => {
    const { deps, admin } = await withAdmin();
    const whoami = await call(deps, "/admin/whoami", { token: admin.token });
    expect(whoami.json).toEqual({ operator: true, role: "admin", email: admin.email, announce: true, manage: true });
  });

  test("an announcer may announce but not manage", async () => {
    const { deps, admin } = await withAdmin();
    const announcer = await newUser(deps, "announcer");
    const added = await call(deps, "/admin/operators", { method: "POST", token: admin.token, body: { email: announcer.email, name: "Ann", role: "announcer" } });
    expect(added.status).toBe(201);
    expect(added.json).toMatchObject({ email: announcer.email, name: "Ann", role: "announcer", from_env: false });

    expect((await call(deps, "/admin/whoami", { token: announcer.token })).json).toMatchObject({ role: "announcer", announce: true, manage: false });
    expect((await call(deps, "/admin/announcements", { token: announcer.token })).status).toBe(200);
    expect((await call(deps, "/admin/stats", { token: announcer.token })).status).toBe(404);
    expect((await call(deps, "/admin/operators", { token: announcer.token })).status).toBe(404);

    const operators = await call(deps, "/admin/operators", { token: admin.token });
    expect(operators.json.operators[0]).toEqual({
      id: "", email: admin.email, name: "Deploy admin", role: "admin", from_env: true, created_at: "0001-01-01T00:00:00Z",
    });
    expect(operators.json.operators.map((o: any) => o.email)).toContain(announcer.email);

    expect((await call(deps, `/admin/operators/${added.json.id}`, { method: "DELETE", token: admin.token })).status).toBe(204);
    expect((await call(deps, "/admin/whoami", { token: announcer.token })).status).toBe(404);
  });

  test("bad operators are refused", async () => {
    const { deps, admin } = await withAdmin();
    const add = (body: object) => call(deps, "/admin/operators", { method: "POST", token: admin.token, body });
    expect((await add({ email: "nope", name: "x", role: "admin" })).status).toBe(400);
    expect((await add({ email: "a@b.co", name: "", role: "admin" })).status).toBe(400);
    expect((await add({ email: "a@b.co", name: "x", role: "root" })).status).toBe(400);
  });
});

describe("stats and expenses", () => {
  test("stats have every section the screen reads", async () => {
    const { deps, admin } = await withAdmin();
    const stats = await call(deps, "/admin/stats", { token: admin.token });
    expect(stats.status).toBe(200);
    for (const key of ["generated_at", "mode", "currency", "totals", "last_7_days", "last_30_days", "revenue", "spending", "signups", "growth"]) {
      expect(stats.json).toHaveProperty(key);
    }
    expect(stats.json.mode).toBe("self-hosted");
    expect(stats.json.signups.some((s: any) => s.email === admin.email)).toBe(true);
  });

  test("expenses are added and deleted", async () => {
    const { deps, admin } = await withAdmin();
    const added = await call(deps, "/admin/expenses", {
      method: "POST", token: admin.token,
      body: { amount_minor: 1500, currency: "USD", category: "hosting", description: "vps", spent_on: "2026-11-01", recurring: "monthly" },
    });
    expect(added.status).toBe(201);
    let stats = await call(deps, "/admin/stats", { token: admin.token });
    expect(stats.json.spending.recent.some((e: any) => e.id === added.json.id)).toBe(true);

    expect((await call(deps, `/admin/expenses/${added.json.id}`, { method: "DELETE", token: admin.token })).status).toBe(204);
    stats = await call(deps, "/admin/stats", { token: admin.token });
    expect(stats.json.spending.recent.some((e: any) => e.id === added.json.id)).toBe(false);

    const badDate = await call(deps, "/admin/expenses", { method: "POST", token: admin.token, body: { amount_minor: 1, spent_on: "11/01/2026" } });
    expect(badDate.json.code).toBe("invalid_date");
  });
});

describe("announcements", () => {
  test("an announcement goes to every subscribed user with a banner", async () => {
    const { deps, admin } = await withAdmin();
    const reader = await newUser(deps, "reader");

    const unconfirmed = await call(deps, "/admin/announcements", { method: "POST", token: admin.token, body: { subject: "Hi", body: "x" } });
    expect(unconfirmed.json.code).toBe("not_confirmed");

    const sent = await call(deps, "/admin/announcements", {
      method: "POST", token: admin.token, body: { subject: "  Shell hooks  ", body: "Secrets load on cd.", confirm: true },
    });
    expect(sent.status).toBe(201);
    expect(sent.json).toMatchObject({ subject: "Shell hooks", body: "Secrets load on cd.", sent_by_email: admin.email });
    await deps.settle();

    const email = deps.mailer.sent.find((m) => m.to === reader.email && m.subject === "Shell hooks")!;
    expect(email.html).toContain("Secrets load on cd.");
    expect(email.headers?.["List-Unsubscribe"]).toBe("<mailto:unsubscribe@envi.test?subject=unsubscribe>");

    const list = await call(deps, "/admin/announcements", { token: admin.token });
    expect(list.json.announcements[0]).toMatchObject({ id: sent.json.id, failures: 0 });
    expect(list.json.recipients).toBeGreaterThanOrEqual(2);
  });

  test("subject and body are required", async () => {
    const { deps, admin } = await withAdmin();
    const reply = await call(deps, "/admin/announcements", { method: "POST", token: admin.token, body: { subject: " ", body: "x", confirm: true } });
    expect(reply.status).toBe(400);
    expect(reply.json.error).toBe("subject is required");
  });
});
