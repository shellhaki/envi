// Billing routes act only on the caller's own personal organization, never one named in the request.

import { fail, readBody, type Ctx } from "../http";
import { BachsError, verifyWebhook } from "../services/bachs";
import * as billing from "../services/billing";
import { applyEvent } from "../services/billingWebhook";

// ownedOrg finds the caller's personal organization and email, or returns the reply to send.
async function ownedOrg(c: Ctx): Promise<{ orgId: string; email: string } | Response> {
  const userId = c.get("userId");
  if (!userId) return fail(c, 403, "forbidden", "billing needs a signed-in person, not a service token");
  const row = await c.env.deps.db
    .one(
      `SELECT o.id, u.email FROM users u
         JOIN memberships m ON m.user_id = u.id AND m.role = 'owner'
         JOIN organizations o ON o.id = m.org_id AND o.type = 'personal'
        WHERE u.id = $1`,
      [userId],
    )
    .catch(() => undefined);
  if (!row) return fail(c, 500, "internal", "account unavailable");
  return { orgId: row.id, email: row.email };
}

// billingError hides the payment provider's reply from the user and logs it instead.
function billingError(c: Ctx, err: unknown) {
  if (err instanceof BachsError) {
    console.error(`billing ${c.req.path}: ${err.message}`);
    return fail(c, 502, "payment_provider", "the payment provider refused the request; try again shortly");
  }
  return fail(c, 400, "billing", (err as Error).message);
}

// GET /billing
export async function overview(c: Ctx) {
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    return c.json(await billing.overview(c.env.deps, org.orgId));
  } catch {
    return fail(c, 500, "internal", "billing unavailable");
  }
}

// POST /billing/payg {currency}. An empty url means a card was already saved and the switch is done.
export async function startPayg(c: Ctx) {
  const body = await readBody(c, { currency: "string" });
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    return c.json({ url: await billing.startPayg(c.env.deps, org.orgId, org.email, body?.currency ?? "") });
  } catch (err) {
    return billingError(c, err);
  }
}

// POST /billing/team
export async function startTeam(c: Ctx) {
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    return c.json({ url: await billing.startTeam(c.env.deps, org.orgId, org.email) });
  } catch (err) {
    return billingError(c, err);
  }
}

// POST /billing/cancel
export async function cancel(c: Ctx) {
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    await billing.cancel(c.env.deps, org.orgId);
  } catch (err) {
    return billingError(c, err);
  }
  return c.body(null, 204);
}

// POST /billing/portal
export async function portal(c: Ctx) {
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    return c.json({ url: await billing.portal(c.env.deps, org.orgId) });
  } catch (err) {
    return billingError(c, err);
  }
}

// POST /billing/retry
export async function retry(c: Ctx) {
  const org = await ownedOrg(c);
  if (org instanceof Response) return org;
  try {
    await billing.retryNow(c.env.deps, org.orgId);
  } catch (err) {
    return billingError(c, err);
  }
  return c.body(null, 204);
}

// POST /webhooks/bachs. Bachs retries on 500 but not on 400, so 400 is only for a bad signature.
export async function webhook(c: Ctx) {
  const deps = c.env.deps;
  if (!deps.bachs) return c.body(null, 404);
  // The signature covers these exact bytes, so read them raw rather than decoding JSON.
  const body = (await c.req.text()).slice(0, 1 << 20);
  let event;
  try {
    event = await verifyWebhook(deps.config.bachs.webhookSecret, c.req.raw.headers, body, deps.now());
  } catch {
    return c.json({ error: "signature does not verify" }, 400);
  }
  try {
    await applyEvent(deps, event);
  } catch {
    return c.json({ error: "could not apply event" }, 500);
  }
  return c.body(null, 200);
}
