// Applying Bachs webhooks. Each event is applied once: its id is recorded in bachs_events.

import type { Deps } from "../deps";
import type { Queryable } from "../db";
import type { BachsEvent } from "./bachs";
import { collaboratorsSql, monthOf, nextAttemptAt, PER_MEMBER } from "./billing";

class Unmatched extends Error {}

export async function applyEvent(deps: Deps, event: BachsEvent): Promise<void> {
  await deps.db.transaction(async (tx) => {
    const fresh = await tx.exec(`INSERT INTO bachs_events (id, type) VALUES ($1, $2) ON CONFLICT DO NOTHING`, [event.id, event.type]);
    if (fresh === 0) return; // already applied
    const data = event.data ?? {};
    try {
      switch (event.type) {
        case "checkout.completed":
          return await checkoutCompleted(deps, tx, data);
        case "payment_method.saved":
          return await cardSaved(tx, data);
        case "customer.subscription.created":
        case "customer.subscription.updated":
        case "customer.subscription.deleted":
          return await subscriptionChanged(deps, tx, event);
        case "collection.succeeded":
        case "collection.failed":
          return await collection(deps, tx, event);
      }
    } catch (err) {
      if (!(err instanceof Unmatched)) throw err;
      console.warn(`bachs event ${event.id} (${event.type}): event matches no organization`);
    }
  });
}

// customerId reads the customer however the event spells it: an id, or an object with "id" or "customer_id".
function customerId(data: any): string {
  if (data.customer_id) return data.customer_id;
  const customer = data.customer;
  if (typeof customer === "string") return customer;
  if (customer && typeof customer === "object") return customer.customer_id || customer.id || "";
  return "";
}

// orgFor finds which organization an event is about.
async function orgFor(tx: Queryable, data: any): Promise<string> {
  const fromMetadata = data.metadata?.org_id;
  if (fromMetadata) {
    const org = await tx.one(`SELECT id FROM organizations WHERE id::text = $1`, [String(fromMetadata)]);
    if (org) return org.id;
  }
  const customer = customerId(data);
  if (customer) {
    const account = await tx.one(`SELECT org_id FROM billing_accounts WHERE bachs_customer_id = $1`, [customer]);
    if (account) return account.org_id;
  }
  throw new Unmatched();
}

async function ensureRow(tx: Queryable, orgId: string) {
  await tx.exec(`INSERT INTO billing_accounts (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, [orgId]);
}

// checkoutCompleted is the only signal for a save-card checkout. It starts pay as you go.
async function checkoutCompleted(deps: Deps, tx: Queryable, data: any) {
  if (data.mode !== "setup") return; // subscription checkouts report through their own events
  const orgId = await orgFor(tx, data);
  await ensureRow(tx, orgId);
  await tx.exec(`UPDATE billing_accounts SET card_saved = true, updated_at = now() WHERE org_id = $1`, [orgId]);
  if (data.metadata?.plan !== "payg") return;

  let currency = String(data.metadata?.currency ?? "").toUpperCase();
  if (!(currency in PER_MEMBER)) currency = "USD";
  await tx.exec(`UPDATE billing_accounts SET plan = 'payg', currency = $2 WHERE org_id = $1`, [orgId, currency]);
  // Everyone already there counts from today.
  await tx.exec(
    `INSERT INTO billable_members (org_id, user_id, month)
     SELECT $1, c.subject_user_id, $2::date FROM (${collaboratorsSql}) c
     ON CONFLICT DO NOTHING`,
    [orgId, monthOf(deps.now())],
  );
}

async function cardSaved(tx: Queryable, data: any) {
  const orgId = await orgFor(tx, data);
  await ensureRow(tx, orgId);
  await tx.exec(
    `UPDATE billing_accounts SET card_saved = true, card_brand = $2, card_last4 = $3, updated_at = now() WHERE org_id = $1`,
    [orgId, data.card_brand ?? "", data.card_last4 ?? ""],
  );
}

async function subscriptionChanged(deps: Deps, tx: Queryable, event: BachsEvent) {
  const data = event.data ?? {};
  const teamProduct = deps.config.bachs.teamProductId;
  if (teamProduct && data.product_id && data.product_id !== teamProduct) return; // only the Team product moves a plan

  const orgId = await orgFor(tx, data);
  await ensureRow(tx, orgId);
  const createdAt = event.created_at ? new Date(event.created_at) : null;
  const account = await tx.one(`SELECT last_event_at FROM billing_accounts WHERE org_id = $1 FOR UPDATE`, [orgId]);
  if (account?.last_event_at && createdAt && createdAt < account.last_event_at) return; // an older state arriving late

  const status = event.type === "customer.subscription.deleted" ? "canceled" : (data.status ?? "");
  const plan = ["canceled", "unpaid", "incomplete_expired"].includes(status) ? "free" : "team";
  await tx.exec(
    `UPDATE billing_accounts
        SET plan = CASE WHEN $3 = 'free' AND plan <> 'team' THEN plan ELSE $3 END,
            subscription_id = COALESCE(NULLIF($2, ''), subscription_id),
            subscription_status = $4,
            cancel_at_period_end = $5,
            current_period_end = $6,
            last_event_at = $7,
            updated_at = now()
      WHERE org_id = $1`,
    [orgId, data.subscription_id ?? "", plan, status, Boolean(data.cancel_at_period_end), data.current_period_end ?? null, createdAt],
  );
}

// collection settles a pay-as-you-go month. The reference is "payg:<org>:<YYYY-MM>:<attempt>".
async function collection(deps: Deps, tx: Queryable, event: BachsEvent) {
  const data = event.data ?? {};
  const reference = String(data.reference ?? "");
  if (!reference.startsWith("payg:")) return;
  const parts = reference.split(":");
  const [, orgId, month, attemptNumber] = parts;
  if (parts.length !== 4 || !/^\d{4}-\d{2}$/.test(month)) throw new Unmatched();

  const charge = await tx.one(
    `SELECT id, attempts FROM payg_charges WHERE org_id::text = $1 AND month = $2::date FOR UPDATE`,
    [orgId, `${month}-01`],
  );
  if (!charge) throw new Unmatched();
  if (attemptNumber !== String(charge.attempts)) return; // a late report about an earlier attempt

  if (event.type === "collection.succeeded") {
    await tx.exec(
      `UPDATE payg_charges SET status = 'succeeded', bachs_charge_id = COALESCE(NULLIF($2, ''), bachs_charge_id),
              next_attempt_at = NULL, last_error = '', updated_at = now()
        WHERE id = $1`,
      [charge.id, data.charge_id ?? ""],
    );
    return;
  }
  await tx.exec(
    `UPDATE payg_charges SET status = 'failed', next_attempt_at = $2, last_error = $3, updated_at = now() WHERE id = $1`,
    [charge.id, nextAttemptAt(deps, charge.attempts), data.reason || "card refused"],
  );
}
