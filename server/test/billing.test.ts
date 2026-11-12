import { describe, expect, test } from "bun:test";
import { signWebhook } from "../src/services/bachs";
import { billingPass } from "../src/services/billing";
import { call, FakeBachs, newProject, newUser, testDeps, type TestDeps } from "./helpers";

const SECRET = "whsec_test";
const bachsConfig = { secretKey: "sk_test_x", webhookSecret: SECRET, teamProductId: "prod_team", returnUrl: "" };

async function billingDeps() {
  const bachs = new FakeBachs();
  const deps = await testDeps({ bachs: bachsConfig }, bachs);
  return { deps, bachs };
}

let eventCounter = 0;

async function deliver(deps: TestDeps, type: string, data: object, extra: object = {}) {
  const body = JSON.stringify({ id: `evt_${Date.now()}_${eventCounter++}`, type, data, ...extra });
  const signature = await signWebhook(SECRET, body, deps.now());
  return call(deps, "/webhooks/bachs", { method: "POST", body, headers: { "X-Bachs-Signature-V2": signature } });
}

async function invite(deps: TestDeps, owner: { token: string }, projectId: string, email: string) {
  return call(deps, `/projects/${projectId}/invitations`, { method: "POST", token: owner.token, body: { email } });
}

// onPayg puts the owner's organization on pay as you go with a saved card, the way the webhooks would.
async function onPayg(deps: TestDeps, orgId: string, currency = "USD") {
  expect((await deliver(deps, "checkout.completed", { mode: "setup", metadata: { org_id: orgId, plan: "payg", currency } })).status).toBe(200);
  await deps.db.exec(`UPDATE billing_accounts SET bachs_customer_id = $2 WHERE org_id = $1`, [orgId, `cus_${orgId}`]);
}

describe("plans", () => {
  test("the free plan cannot invite anyone", async () => {
    const { deps } = await billingDeps();
    const owner = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const refused = await invite(deps, owner, project.ID, "friend@example.com");
    expect(refused.status).toBe(402);
    expect(refused.json).toEqual({ code: "payment_required", error: expect.stringContaining("free plan"), plan: "free" });
  });

  test("without billing configured, invitations are free", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const { project } = await newProject(deps, owner);
    expect((await invite(deps, owner, project.ID, "friend@example.com")).status).toBe(201);
    expect((await call(deps, "/billing", { token: owner.token })).json.enabled).toBe(false);
    expect((await call(deps, "/billing/team", { method: "POST", token: owner.token })).status).toBe(400);
    expect((await call(deps, "/webhooks/bachs", { method: "POST", body: "{}" })).status).toBe(404);
  });

  test("pay as you go starts with a save-card checkout", async () => {
    const { deps, bachs } = await billingDeps();
    const owner = await newUser(deps);
    const started = await call(deps, "/billing/payg", { method: "POST", token: owner.token, body: { currency: "USD" } });
    expect(started.json).toEqual({ url: "https://pay.test/save-card" });
    expect(bachs.calls).toEqual([`customer ${owner.email}`, `save-card cus_${owner.email}`]);

    expect((await call(deps, "/billing/payg", { method: "POST", token: owner.token, body: { currency: "EUR" } })).status).toBe(400);

    await deliver(deps, "checkout.completed", { mode: "setup", customer: `cus_${owner.email}`, metadata: { plan: "payg", currency: "usd" } });
    const overview = await call(deps, "/billing", { token: owner.token });
    expect(overview.json.account).toMatchObject({ plan: "payg", currency: "USD", card_saved: true });
    expect(overview.json.account.customerId).toBeUndefined();

    const again = await call(deps, "/billing/payg", { method: "POST", token: owner.token, body: { currency: "NGN" } });
    expect(again.json).toEqual({ url: "" });
    expect((await call(deps, "/billing", { token: owner.token })).json.account.currency).toBe("NGN");
  });

  test("pay as you go lets people in up to the cap and counts them", async () => {
    const { deps } = await billingDeps();
    const owner = await newUser(deps);
    const guest = await newUser(deps);
    const { project } = await newProject(deps, owner);
    await onPayg(deps, owner.orgId);
    await deps.db.exec(`UPDATE billing_accounts SET payg_cap = 1 WHERE org_id = $1`, [owner.orgId]);

    const invited = await invite(deps, owner, project.ID, guest.email);
    expect(invited.status).toBe(201);
    expect((await call(deps, "/invitations/accept", { method: "POST", token: guest.token, body: { token: invited.json.Token } })).status).toBe(204);

    const overview = await call(deps, "/billing", { token: owner.token });
    expect(overview.json.people).toEqual([{ email: guest.email }]);
    expect(overview.json.counted_this_month).toBe(1);
    expect(overview.json.projected_minor).toBe(40);

    const capped = await invite(deps, owner, project.ID, "third@example.com");
    expect(capped.status).toBe(402);
    expect(capped.json.code).toBe("payg_cap_reached");
  });

  test("Team checkout, cancel and portal", async () => {
    const { deps, bachs } = await billingDeps();
    const owner = await newUser(deps);
    expect((await call(deps, "/billing/team", { method: "POST", token: owner.token })).json).toEqual({ url: "https://pay.test/team" });
    expect(bachs.calls).toContain(`subscribe cus_${owner.email} prod_team`);

    await deliver(deps, "customer.subscription.created", {
      customer_id: `cus_${owner.email}`, subscription_id: "sub_1", product_id: "prod_team", status: "active",
    }, { created_at: new Date().toISOString() });
    expect((await call(deps, "/billing", { token: owner.token })).json.account).toMatchObject({ plan: "team", subscription_status: "active" });
    expect((await call(deps, "/billing/team", { method: "POST", token: owner.token })).json.error).toContain("already on Team");

    const { project } = await newProject(deps, owner);
    expect((await invite(deps, owner, project.ID, "anyone@example.com")).status).toBe(201);

    expect((await call(deps, "/billing/cancel", { method: "POST", token: owner.token })).status).toBe(204);
    expect(bachs.calls).toContain("cancel sub_1");
    expect((await call(deps, "/billing", { token: owner.token })).json.account.cancel_at_period_end).toBe(true);

    expect((await call(deps, "/billing/portal", { method: "POST", token: owner.token })).json).toEqual({ url: `https://pay.test/portal/cus_${owner.email}` });

    await deliver(deps, "customer.subscription.deleted", { customer_id: `cus_${owner.email}`, product_id: "prod_team" }, { created_at: new Date().toISOString() });
    expect((await call(deps, "/billing", { token: owner.token })).json.account).toMatchObject({ plan: "free", subscription_status: "canceled" });
  });

  test("an older subscription event arriving late is ignored", async () => {
    const { deps } = await billingDeps();
    const owner = await newUser(deps);
    const data = { metadata: { org_id: owner.orgId }, subscription_id: `sub_${owner.orgId}`, product_id: "prod_team" };
    await deliver(deps, "customer.subscription.updated", { ...data, status: "active" }, { created_at: "2026-11-02T00:00:00Z" });
    await deliver(deps, "customer.subscription.updated", { ...data, status: "canceled" }, { created_at: "2026-11-01T00:00:00Z" });
    expect((await call(deps, "/billing", { token: owner.token })).json.account.plan).toBe("team");
  });

  test("the free plan has nothing to cancel or retry", async () => {
    const { deps } = await billingDeps();
    const owner = await newUser(deps);
    expect((await call(deps, "/billing/cancel", { method: "POST", token: owner.token })).json.code).toBe("billing");
    expect((await call(deps, "/billing/retry", { method: "POST", token: owner.token })).status).toBe(400);
    expect((await call(deps, "/billing/portal", { method: "POST", token: owner.token })).status).toBe(400);
  });
});

describe("webhooks", () => {
  test("a bad or old signature is a 400", async () => {
    const { deps } = await billingDeps();
    const body = JSON.stringify({ id: "evt_bad", type: "checkout.completed", data: {} });
    expect((await call(deps, "/webhooks/bachs", { method: "POST", body })).status).toBe(400);
    const wrong = await signWebhook("other-secret", body, new Date());
    expect((await call(deps, "/webhooks/bachs", { method: "POST", body, headers: { "X-Bachs-Signature-V2": wrong } })).status).toBe(400);
    const old = await signWebhook(SECRET, body, new Date(Date.now() - 10 * 60 * 1000));
    expect((await call(deps, "/webhooks/bachs", { method: "POST", body, headers: { "X-Bachs-Signature-V2": old } })).status).toBe(400);
  });

  test("an event is applied once", async () => {
    const { deps } = await billingDeps();
    const owner = await newUser(deps);
    const body = JSON.stringify({ id: `evt_once_${owner.orgId}`, type: "payment_method.saved", data: { metadata: { org_id: owner.orgId }, card_brand: "visa", card_last4: "4242" } });
    const send = async () => call(deps, "/webhooks/bachs", { method: "POST", body, headers: { "X-Bachs-Signature-V2": await signWebhook(SECRET, body, new Date()) } });
    expect((await send()).status).toBe(200);
    await deps.db.exec(`UPDATE billing_accounts SET card_last4 = '0000' WHERE org_id = $1`, [owner.orgId]);
    expect((await send()).status).toBe(200);
    expect((await call(deps, "/billing", { token: owner.token })).json.account.card_last4).toBe("0000");
  });

  test("an event for nobody is acknowledged", async () => {
    const { deps } = await billingDeps();
    expect((await deliver(deps, "payment_method.saved", { customer: "cus_nobody" })).status).toBe(200);
    expect((await deliver(deps, "collection.succeeded", { reference: "payg:not-a-uuid:bad:1" })).status).toBe(200);
  });
});

describe("the monthly charge", () => {
  async function lastMonthOnPayg(members: number, currency = "USD") {
    const { deps, bachs } = await billingDeps();
    const owner = await newUser(deps);
    await onPayg(deps, owner.orgId, currency);
    for (let i = 0; i < members; i++) {
      const guest = await newUser(deps);
      await deps.db.exec(`INSERT INTO billable_members (org_id, user_id, month) VALUES ($1, $2, '2026-10-01')`, [owner.orgId, guest.userId]);
    }
    deps.now = () => new Date("2026-11-03T12:00:00Z");
    return { deps, bachs, owner };
  }

  const chargesFor = (deps: TestDeps, orgId: string) =>
    deps.db.query(`SELECT month::text, members, amount_minor, carried_minor, status, attempts, next_attempt_at FROM payg_charges WHERE org_id = $1 ORDER BY month`, [orgId]);

  test("a finished month is charged with an idempotency key per attempt", async () => {
    const { deps, bachs, owner } = await lastMonthOnPayg(10);
    await billingPass(deps);
    expect(bachs.calls).toContain(`charge cus_${owner.orgId} 400 USD payg:${owner.orgId}:2026-10:1`);
    expect(await chargesFor(deps, owner.orgId)).toEqual([
      { month: "2026-10-01", members: 10, amount_minor: 400, carried_minor: 0, status: "processing", attempts: 1, next_attempt_at: null },
    ]);

    await billingPass(deps);
    expect(bachs.calls.filter((c) => c.startsWith("charge"))).toHaveLength(1);

    expect((await deliver(deps, "collection.succeeded", { reference: `payg:${owner.orgId}:2026-10:1`, charge_id: "ch_ok" })).status).toBe(200);
    expect((await chargesFor(deps, owner.orgId))[0].status).toBe("succeeded");
  });

  test("a small month is carried into the next", async () => {
    const { deps, bachs, owner } = await lastMonthOnPayg(2);
    await billingPass(deps);
    expect(bachs.calls.some((c) => c.startsWith("charge"))).toBe(false);
    expect((await chargesFor(deps, owner.orgId))[0]).toMatchObject({ amount_minor: 80, status: "carried" });

    await deps.db.exec(`INSERT INTO billable_members (org_id, user_id, month) SELECT $1, user_id, '2026-11-01' FROM billable_members WHERE org_id = $1`, [owner.orgId]);
    for (let i = 0; i < 6; i++) {
      const guest = await newUser(deps);
      await deps.db.exec(`INSERT INTO billable_members (org_id, user_id, month) VALUES ($1, $2, '2026-11-01')`, [owner.orgId, guest.userId]);
    }
    deps.now = () => new Date("2026-12-01T01:00:00Z");
    await billingPass(deps);
    expect(bachs.calls).toContain(`charge cus_${owner.orgId} 400 USD payg:${owner.orgId}:2026-11:1`);
    expect((await chargesFor(deps, owner.orgId)).map((c: any) => c.status)).toEqual(["settled", "processing"]);
  });

  test("a refused charge is retried after 3 days, then 7, then stops", async () => {
    const { deps, bachs, owner } = await lastMonthOnPayg(10);
    bachs.failCharges = true;
    await billingPass(deps);
    let [charge] = await chargesFor(deps, owner.orgId);
    expect(charge).toMatchObject({ status: "failed", attempts: 1 });
    expect(charge.next_attempt_at.toISOString()).toBe("2026-11-06T12:00:00.000Z");

    const { project } = await newProject(deps, owner);
    expect((await invite(deps, owner, project.ID, "blocked@example.com")).status).toBe(201);

    deps.now = () => new Date("2026-11-06T13:00:00Z");
    await billingPass(deps);
    [charge] = await chargesFor(deps, owner.orgId);
    expect(charge.attempts).toBe(2);

    deps.now = () => new Date("2026-11-14T13:00:00Z");
    await billingPass(deps);
    [charge] = await chargesFor(deps, owner.orgId);
    expect(charge).toMatchObject({ status: "failed", attempts: 3, next_attempt_at: null });

    const blocked = await invite(deps, owner, project.ID, "another@example.com");
    expect(blocked.json.code).toBe("payg_past_due");
    expect((await call(deps, "/billing", { token: owner.token })).json.past_due).toBe(true);

    bachs.failCharges = false;
    expect((await call(deps, "/billing/retry", { method: "POST", token: owner.token })).status).toBe(204);
    [charge] = await chargesFor(deps, owner.orgId);
    expect(charge).toMatchObject({ status: "processing", attempts: 3 });
  });

  test("a failed collection report schedules the retry", async () => {
    const { deps, owner } = await lastMonthOnPayg(10);
    await billingPass(deps);
    await deliver(deps, "collection.failed", { reference: `payg:${owner.orgId}:2026-10:1`, reason: "insufficient funds" });
    const [charge] = await deps.db.query(`SELECT status, last_error FROM payg_charges WHERE org_id = $1`, [owner.orgId]);
    expect(charge).toEqual({ status: "failed", last_error: "insufficient funds" });
  });
});
