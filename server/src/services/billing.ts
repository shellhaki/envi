// Billing decides what an organization owes and whether it may add another person.
// Bachs moves the money; everything about what is owed is decided here, against Envi's own database.

import type { Deps } from "../deps";
import type { Queryable } from "../db";
import { BachsError } from "./bachs";

export const PER_MEMBER: Record<string, number> = { USD: 40, NGN: 60000 }; // $0.40, ₦600 per extra person per month
export const MINIMUM: Record<string, number> = { USD: 300, NGN: 0 }; // smallest monthly charge worth making
export const TEAM_PRICE_MINOR = 400;
export const MAX_ATTEMPTS = 3; // at once, after 3 days, after 7
const RETRY_AFTER_DAYS = [3, 7];

// Refusal is why an organization may not add somebody; the API answers it with 402.
export class Refusal extends Error {
  constructor(public code: string, public plan: string, message: string) {
    super(message);
  }
  toJSON() {
    return { code: this.code, error: this.message, plan: this.plan };
  }
}

export class BillingError extends Error {}

export type Account = {
  org_id: string;
  plan: string;
  currency: string;
  card_saved: boolean;
  card_brand: string;
  card_last4: string;
  subscription_status: string;
  cancel_at_period_end: boolean;
  current_period_end: Date | null;
  payg_cap: number;
};

type AccountRow = Account & { customerId: string; subscriptionId: string };

// collaboratorsSql selects the extra people in org $1: grant holders on its projects who aren't members.
export const collaboratorsSql = `
  SELECT DISTINCT g.subject_user_id
    FROM access_grants g
    JOIN projects p ON p.id = g.project_id
   WHERE p.org_id = $1
     AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = $1 AND m.user_id = g.subject_user_id)`;

// monthOf is the first day of the date's month, as stored: "2026-11-01".
export function monthOf(date: Date): string {
  return date.toISOString().slice(0, 8) + "01";
}

// loadAccount reads an organization's billing state. No row means free.
export async function loadAccount(db: Queryable, orgId: string): Promise<AccountRow> {
  const row = await db.one(
    `SELECT plan, currency, bachs_customer_id, card_saved, card_brand, card_last4, subscription_id,
            subscription_status, cancel_at_period_end, current_period_end, payg_cap
       FROM billing_accounts WHERE org_id = $1`,
    [orgId],
  );
  return {
    org_id: orgId,
    plan: row?.plan ?? "free",
    currency: row?.currency ?? "USD",
    card_saved: row?.card_saved ?? false,
    card_brand: row?.card_brand ?? "",
    card_last4: row?.card_last4 ?? "",
    subscription_status: row?.subscription_status ?? "",
    cancel_at_period_end: row?.cancel_at_period_end ?? false,
    current_period_end: row?.current_period_end ?? null,
    payg_cap: row?.payg_cap ?? 20,
    customerId: row?.bachs_customer_id ?? "",
    subscriptionId: row?.subscription_id ?? "",
  };
}

// teamActive: on Team and paid for (past_due is still being retried, so it counts).
function teamActive(a: Account) {
  return a.plan === "team" && ["active", "trialing", "past_due"].includes(a.subscription_status);
}

function publicAccount({ customerId, subscriptionId, ...account }: AccountRow): Account {
  return account;
}

async function ensureRow(db: Queryable, orgId: string) {
  await db.exec(`INSERT INTO billing_accounts (org_id) VALUES ($1) ON CONFLICT DO NOTHING`, [orgId]);
}

async function pastDue(db: Queryable, orgId: string): Promise<boolean> {
  const row = await db.one(
    `SELECT EXISTS (SELECT 1 FROM payg_charges WHERE org_id = $1 AND status = 'failed' AND attempts >= $2) AS due`,
    [orgId, MAX_ATTEMPTS],
  );
  return row!.due;
}

// allowNewPerson throws a Refusal if orgId may not give email access.
export async function allowNewPerson(deps: Deps, orgId: string, email: string): Promise<void> {
  if (!deps.bachs) return;
  const { db } = deps;
  const alreadyIn = await db.one(
    `SELECT EXISTS (
       SELECT 1 FROM users u
        WHERE lower(u.email) = lower($2)
          AND (u.id IN (${collaboratorsSql})
               OR EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = $1 AND m.user_id = u.id))
     ) AS ok`,
    [orgId, email],
  );
  if (alreadyIn!.ok) return;

  const account = await loadAccount(db, orgId);
  if (teamActive(account)) return;
  if (account.plan !== "payg") {
    throw new Refusal("payment_required", "free",
      "the free plan is for one person; choose pay as you go or Team on the billing page to add anyone else");
  }
  if (!account.card_saved) {
    throw new Refusal("payment_required", "payg",
      "pay as you go needs a saved card before anyone can be added; finish setting it up on the billing page");
  }
  if (await pastDue(db, orgId)) {
    throw new Refusal("payg_past_due", "payg",
      "a pay-as-you-go charge has failed; update the card on the billing page before adding anyone else");
  }
  const extras = await db.one(`SELECT count(*) AS n FROM (${collaboratorsSql}) c`, [orgId]);
  if (extras!.n >= account.payg_cap) {
    throw new Refusal("payg_cap_reached", "payg", `pay as you go is capped at ${account.payg_cap} extra people; Team has no cap`);
  }
}

// recordPerson notes that userId counts for orgId this month.
export async function recordPerson(deps: Deps, orgId: string, userId: string): Promise<void> {
  if (!deps.bachs) return;
  await deps.db.exec(
    `INSERT INTO billable_members (org_id, user_id, month)
     SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM billing_accounts WHERE org_id = $1 AND plan = 'payg')
     ON CONFLICT DO NOTHING`,
    [orgId, userId, monthOf(deps.now())],
  );
}

// overview is everything the billing page shows.
export async function overview(deps: Deps, orgId: string) {
  const { db } = deps;
  const account = await loadAccount(db, orgId);
  const people = await db.query<{ email: string }>(
    `SELECT u.email FROM users u WHERE u.id IN (${collaboratorsSql}) ORDER BY u.email`,
    [orgId],
  );
  const counted = await db.one(
    `SELECT count(*) AS n FROM billable_members WHERE org_id = $1 AND month = $2`,
    [orgId, monthOf(deps.now())],
  );
  // This month so far: everyone counted, plus anyone here now the sweep hasn't counted yet.
  const projected = account.plan === "payg" ? Math.max(counted!.n, people.length) * PER_MEMBER[account.currency] : 0;
  const charges = await db.query(
    `SELECT to_char(month, 'YYYY-MM') AS month, currency, members, amount_minor, carried_minor, status, attempts, last_error
       FROM payg_charges WHERE org_id = $1 ORDER BY month DESC LIMIT 12`,
    [orgId],
  );
  return {
    enabled: deps.bachs !== null,
    account: publicAccount(account),
    people,
    counted_this_month: counted!.n,
    projected_minor: projected,
    past_due: await pastDue(db, orgId),
    charges,
    prices: { per_member: PER_MEMBER, minimum: MINIMUM, team: TEAM_PRICE_MINOR },
    team_available: deps.config.bachs.teamProductId !== "",
  };
}

function enabledBachs(deps: Deps) {
  if (!deps.bachs) throw new BillingError("billing is not enabled on this instance");
  return deps.bachs;
}

function returnUrls(deps: Deps) {
  const base = (deps.config.bachs.returnUrl || deps.config.webUrl).replace(/\/+$/, "");
  return { success: `${base}/billing/success`, cancel: `${base}/dashboard/billing?checkout=cancelled` };
}

// customerFor returns the organization's Bachs customer, creating it the first time.
async function customerFor(deps: Deps, orgId: string, email: string): Promise<string> {
  const bachs = enabledBachs(deps);
  await ensureRow(deps.db, orgId);
  return deps.db.transaction(async (tx) => {
    const row = await tx.one(`SELECT bachs_customer_id FROM billing_accounts WHERE org_id = $1 FOR UPDATE`, [orgId]);
    if (row?.bachs_customer_id) return row.bachs_customer_id as string;
    const id = await bachs.createCustomer(email, email, { org_id: orgId });
    await tx.exec(`UPDATE billing_accounts SET bachs_customer_id = $2, updated_at = now() WHERE org_id = $1`, [orgId, id]);
    return id;
  });
}

// startPayg moves an organization onto pay as you go. Returns a checkout URL, or "" if a card was already saved.
export async function startPayg(deps: Deps, orgId: string, email: string, currency: string): Promise<string> {
  const bachs = enabledBachs(deps);
  if (!(currency in PER_MEMBER)) throw new BillingError("currency must be USD or NGN");
  const account = await loadAccount(deps.db, orgId);
  if (teamActive(account) && !account.cancel_at_period_end) {
    throw new BillingError("cancel Team first; pay as you go starts when it ends");
  }
  if (account.card_saved) {
    await ensureRow(deps.db, orgId);
    await deps.db.exec(`UPDATE billing_accounts SET plan = 'payg', currency = $2, updated_at = now() WHERE org_id = $1`, [orgId, currency]);
    await sweepOrg(deps, orgId);
    return "";
  }
  const customerId = await customerFor(deps, orgId, email);
  const { success, cancel } = returnUrls(deps);
  const checkout = await bachs.saveCardCheckout(customerId, success, cancel, { org_id: orgId, plan: "payg", currency });
  return checkout.checkout_url;
}

// startTeam returns a checkout for the Team subscription.
export async function startTeam(deps: Deps, orgId: string, email: string): Promise<string> {
  const bachs = enabledBachs(deps);
  const productId = deps.config.bachs.teamProductId;
  if (!productId) throw new BillingError("Team is not set up on this instance yet");
  if (teamActive(await loadAccount(deps.db, orgId))) throw new BillingError("this organization is already on Team");
  const customerId = await customerFor(deps, orgId, email);
  const { success, cancel } = returnUrls(deps);
  const checkout = await bachs.subscriptionCheckout(customerId, productId, success, cancel, { org_id: orgId, plan: "team" });
  return checkout.checkout_url;
}

// cancel ends the current paid plan: Team at the end of the paid period, pay as you go at once.
export async function cancel(deps: Deps, orgId: string): Promise<void> {
  const bachs = enabledBachs(deps);
  const account = await loadAccount(deps.db, orgId);
  if (teamActive(account)) {
    if (!account.subscriptionId) throw new BillingError("no subscription id recorded for this organization");
    await bachs.cancelSubscription(account.subscriptionId);
    await deps.db.exec(`UPDATE billing_accounts SET cancel_at_period_end = true, updated_at = now() WHERE org_id = $1`, [orgId]);
    return;
  }
  if (account.plan === "payg") {
    await deps.db.exec(`UPDATE billing_accounts SET plan = 'free', updated_at = now() WHERE org_id = $1`, [orgId]);
    return;
  }
  throw new BillingError("nothing to cancel: this organization is on the free plan");
}

// portal returns a Bachs customer portal link for card updates and invoices.
export async function portal(deps: Deps, orgId: string): Promise<string> {
  const bachs = enabledBachs(deps);
  const account = await loadAccount(deps.db, orgId);
  if (!account.customerId) throw new BillingError("no payment details yet; choose a plan first");
  return bachs.portalUrl(account.customerId);
}

// retryNow charges a failed month again immediately, e.g. after the owner fixes their card.
export async function retryNow(deps: Deps, orgId: string): Promise<void> {
  enabledBachs(deps);
  const changed = await deps.db.exec(
    `UPDATE payg_charges SET attempts = LEAST(attempts, $2 - 1), next_attempt_at = now(), updated_at = now()
      WHERE org_id = $1 AND status = 'failed'`,
    [orgId, MAX_ATTEMPTS],
  );
  if (changed === 0) throw new BillingError("there is no failed charge to retry");
  await retryDue(deps);
}

// sweepOrg counts everyone currently in the organization for this month.
export async function sweepOrg(deps: Deps, orgId: string) {
  await deps.db.exec(
    `INSERT INTO billable_members (org_id, user_id, month)
     SELECT $1, c.subject_user_id, $2::date FROM (${collaboratorsSql}) c
     ON CONFLICT DO NOTHING`,
    [orgId, monthOf(deps.now())],
  );
}

// --- the monthly charge job ----------------------------------------------------

const JOB_LOCK = 0x656e766962696c6cn; // "envibill"

// billingPass counts people, closes finished months and retries failed charges. Runs hourly.
export async function billingPass(deps: Deps): Promise<void> {
  if (!deps.bachs) return;
  await deps.db.withClient(async (client) => {
    const lock = await client.one(`SELECT pg_try_advisory_lock($1) AS got`, [JOB_LOCK.toString()]);
    if (!lock!.got) return; // another server is mid-pass
    try {
      await sweep(deps);
      await closeMonths(deps);
      await retryDue(deps);
    } finally {
      await client.query(`SELECT pg_advisory_unlock($1)`, [JOB_LOCK.toString()]);
    }
  });
}

// sweep counts everyone in every pay-as-you-go organization for this month.
async function sweep(deps: Deps) {
  await deps.db.exec(
    `INSERT INTO billable_members (org_id, user_id, month)
     SELECT b.org_id, g.subject_user_id, $1::date
       FROM billing_accounts b
       JOIN projects p ON p.org_id = b.org_id
       JOIN access_grants g ON g.project_id = p.id
      WHERE b.plan = 'payg'
        AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = b.org_id AND m.user_id = g.subject_user_id)
     ON CONFLICT DO NOTHING`,
    [monthOf(deps.now())],
  );
}

// closeMonths creates the charge for every finished month that has none yet.
async function closeMonths(deps: Deps) {
  const open = await deps.db.query(
    `SELECT bm.org_id, bm.month::text AS month, count(*) AS members
       FROM billable_members bm
      WHERE bm.month < $1
        AND NOT EXISTS (SELECT 1 FROM payg_charges pc WHERE pc.org_id = bm.org_id AND pc.month = bm.month)
      GROUP BY bm.org_id, bm.month
      ORDER BY bm.month`,
    [monthOf(deps.now())],
  );
  for (const month of open) {
    await closeMonth(deps, month.org_id, month.month, month.members).catch((err) => {
      console.error(`billing: closing ${month.month.slice(0, 7)} for ${month.org_id}: ${err.message}`);
    });
  }
}

async function closeMonth(deps: Deps, orgId: string, month: string, members: number) {
  const charge = await deps.db.transaction(async (tx) => {
    const { currency } = await loadAccount(tx, orgId);
    const amount = members * PER_MEMBER[currency];
    // Earlier months in the same currency that were too small to charge on their own.
    const carried = await tx.one(
      `SELECT coalesce(sum(amount_minor + carried_minor), 0) AS total FROM payg_charges
        WHERE org_id = $1 AND status = 'carried' AND currency = $2`,
      [orgId, currency],
    );
    const status = amount + carried!.total < MINIMUM[currency] ? "carried" : "processing";
    const row = await tx.one(
      `INSERT INTO payg_charges (org_id, month, currency, members, amount_minor, carried_minor, status)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
       ON CONFLICT (org_id, month) DO NOTHING
       RETURNING id`,
      [orgId, month, currency, members, amount, carried!.total, status],
    );
    if (!row) return undefined; // another pass got here first
    await tx.exec(
      `UPDATE payg_charges SET status = 'settled', settled_by = $3, updated_at = now()
        WHERE org_id = $1 AND status = 'carried' AND currency = $2 AND id <> $3`,
      [orgId, currency, row.id],
    );
    return { id: row.id as string, status };
  });
  if (charge?.status === "processing") await attempt(deps, charge.id);
}

// retryDue charges again every failed month whose wait is over.
async function retryDue(deps: Deps) {
  const due = await deps.db.query(
    `SELECT id FROM payg_charges WHERE status = 'failed' AND attempts < $1 AND next_attempt_at <= $2`,
    [MAX_ATTEMPTS, deps.now()],
  );
  for (const { id } of due) {
    await attempt(deps, id).catch((err) => console.error(`billing: retrying ${id}: ${err.message}`));
  }
}

// attempt sends one charge. The idempotency key ends with the attempt number, so each retry is a new charge.
async function attempt(deps: Deps, chargeId: string) {
  const charge = await deps.db.one(
    `UPDATE payg_charges SET attempts = attempts + 1, status = 'processing', updated_at = now()
      WHERE id = $1 AND status IN ('processing', 'failed')
     RETURNING org_id, month::text AS month, currency, amount_minor, carried_minor, attempts`,
    [chargeId],
  );
  if (!charge) return;
  const account = await loadAccount(deps.db, charge.org_id);
  if (!account.customerId || !account.card_saved) return fail(deps, chargeId, charge.attempts, "no saved card for this organization");

  const key = `payg:${charge.org_id}:${charge.month.slice(0, 7)}:${charge.attempts}`;
  const monthName = new Date(charge.month + "T00:00:00Z").toLocaleDateString("en-US", { month: "long", year: "numeric", timeZone: "UTC" });
  try {
    const result = await deps.bachs!.chargeSavedCard(
      account.customerId, charge.amount_minor + charge.carried_minor, charge.currency, `Envi pay as you go, ${monthName}`, key,
    );
    if (result.status.toLowerCase() === "failed") return fail(deps, chargeId, charge.attempts, "card refused");
    await deps.db.exec(`UPDATE payg_charges SET bachs_charge_id = $2, updated_at = now() WHERE id = $1`, [chargeId, result.id]);
  } catch (err) {
    return fail(deps, chargeId, charge.attempts, err instanceof BachsError ? err.message : String((err as Error).message));
  }
}

// fail records a refused attempt and schedules the next, if there is one.
export async function fail(deps: Deps, chargeId: string, attempts: number, reason: string, db: Queryable = deps.db) {
  await db.exec(
    `UPDATE payg_charges SET status = 'failed', next_attempt_at = $2, last_error = $3, updated_at = now() WHERE id = $1`,
    [chargeId, nextAttemptAt(deps, attempts), reason],
  );
}

export function nextAttemptAt(deps: Deps, attempts: number): Date | null {
  if (attempts >= MAX_ATTEMPTS || attempts - 1 >= RETRY_AFTER_DAYS.length) return null;
  return new Date(deps.now().getTime() + RETRY_AFTER_DAYS[attempts - 1] * 24 * 60 * 60 * 1000);
}
