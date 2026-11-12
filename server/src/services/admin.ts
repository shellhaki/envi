// Instance-wide figures and tools for the operator screen: users, usage, revenue, costs, operators.

import type { Db } from "../db";
import { Invalid } from "../errors";
import { TEAM_PRICE_MINOR } from "./billing";

export const ROLE_ADMIN = "admin";
export const ROLE_ANNOUNCER = "announcer";

export const mayAnnounce = (role: string) => role === ROLE_ADMIN || role === ROLE_ANNOUNCER;
export const mayManage = (role: string) => role === ROLE_ADMIN;

// Dates are returned the way the Go server wrote them: "2026-11-12T00:00:00Z".
const asTime = (column: string) => `to_char(${column}, 'YYYY-MM-DD"T00:00:00Z"')`;

// snapshot is everything the admin screen shows, in one round trip.
export async function snapshot(db: Db, mode: string, currency: string) {
  return {
    generated_at: new Date(),
    mode,
    currency,
    totals: await totals(db),
    last_7_days: await recent(db, 7),
    last_30_days: await recent(db, 30),
    revenue: await revenue(db),
    spending: await spending(db, 25),
    signups: await db.query(
      `SELECT u.email, coalesce(u.name, '') AS name, u.created_at,
              (SELECT count(*) FROM projects p JOIN memberships m ON m.org_id = p.org_id WHERE m.user_id = u.id) AS projects,
              (SELECT count(*) FROM secrets s
                 JOIN environments e ON e.id = s.environment_id
                 JOIN projects p ON p.id = e.project_id
                 JOIN memberships m ON m.org_id = p.org_id
                WHERE m.user_id = u.id AND s.deleted_at IS NULL) AS secrets
         FROM users u
        ORDER BY u.created_at DESC
        LIMIT 50`,
    ),
    // One row per day, including days with no sign-ups.
    growth: await db.query(
      `SELECT ${asTime("d")} AS day, coalesce(c.n, 0) AS users
         FROM generate_series(current_date - 29, current_date, '1 day') AS d
         LEFT JOIN (
           SELECT created_at::date AS day, count(*) AS n FROM users
            WHERE created_at >= current_date - 29 GROUP BY 1
         ) c ON c.day = d::date
        ORDER BY d`,
    ),
  };
}

async function totals(db: Db) {
  return db.one(
    `SELECT
       (SELECT count(*) FROM users) AS users,
       (SELECT count(*) FROM organizations) AS organizations,
       (SELECT count(*) FROM organizations WHERE type = 'team') AS team_orgs,
       (SELECT count(*) FROM projects) AS projects,
       (SELECT count(*) FROM environments) AS environments,
       (SELECT count(*) FROM environments WHERE is_production) AS production_envs,
       (SELECT count(*) FROM secrets WHERE deleted_at IS NULL) AS secrets,
       (SELECT count(*) FROM secret_versions) AS secret_versions,
       (SELECT count(*) FROM kv_entries) AS kv_entries,
       (SELECT count(*) FROM access_grants) AS grants,
       (SELECT count(*) FROM invitations WHERE status = 'pending') AS pending_invites,
       (SELECT count(*) FROM service_identities) AS service_tokens,
       (SELECT count(*) FROM api_tokens WHERE user_id IS NOT NULL AND revoked_at IS NULL) AS api_keys`,
  );
}

async function recent(db: Db, days: number) {
  return db.one(
    `SELECT
       (SELECT count(*) FROM users WHERE created_at >= $1) AS new_users,
       (SELECT count(*) FROM projects WHERE created_at >= $1) AS new_projects,
       (SELECT count(*) FROM audit_events WHERE created_at >= $1 AND action IN ('secret.write', 'secret.delete')) AS secret_writes,
       (SELECT count(*) FROM audit_events WHERE created_at >= $1 AND action = 'secret.read') AS secret_reads,
       (SELECT count(DISTINCT org_id) FROM audit_events WHERE created_at >= $1) AS active_orgs,
       (SELECT count(DISTINCT actor_id) FROM audit_events WHERE created_at >= $1 AND actor_id IS NOT NULL) AS active_people`,
    [new Date(Date.now() - days * 24 * 60 * 60 * 1000)],
  );
}

// revenue: Team counts as MRR; pay as you go counts the money actually collected, per currency.
async function revenue(db: Db) {
  const counts = await db.one(
    `SELECT
       (SELECT count(*) FROM billing_accounts WHERE plan = 'team') AS team_orgs,
       (SELECT count(*) FROM billing_accounts WHERE plan = 'payg') AS payg_orgs,
       (SELECT count(*) FROM billing_accounts WHERE subscription_status = 'trialing') AS trial_orgs,
       (SELECT count(*) FROM organizations) AS orgs,
       (SELECT count(*) FROM payg_charges WHERE status = 'failed') AS failed_charges`,
  );
  const paid = counts!.team_orgs + counts!.payg_orgs;
  return {
    available: true,
    mrr: (counts!.team_orgs * TEAM_PRICE_MINOR) / 100,
    paid_orgs: paid,
    free_orgs: counts!.orgs - paid,
    trial_orgs: counts!.trial_orgs,
    team_orgs: counts!.team_orgs,
    payg_orgs: counts!.payg_orgs,
    collected: await db.query(
      `SELECT currency,
              coalesce(sum(amount_minor + carried_minor) FILTER (WHERE updated_at >= date_trunc('month', now())), 0) AS this_month_minor,
              coalesce(sum(amount_minor + carried_minor), 0) AS all_time_minor
         FROM payg_charges
        WHERE status = 'succeeded'
        GROUP BY currency
        ORDER BY currency`,
    ),
    failed_charges: counts!.failed_charges,
  };
}

// --- expenses ------------------------------------------------------------------

export type ExpenseInput = {
  amount_minor: number;
  currency: string;
  category: string;
  description: string;
  spent_on: string; // YYYY-MM-DD, or "" for today
  recurring: string;
};

const expenseColumns = `id, amount_minor, currency, category, description, ${asTime("spent_on")} AS spent_on, recurring, created_at`;

export async function addExpense(db: Db, operatorId: string, input: ExpenseInput) {
  if (!(input.amount_minor > 0)) throw new Invalid("amount must be greater than zero");
  const currency = input.currency.trim().toUpperCase();
  if (currency !== "NGN" && currency !== "USD") throw new Invalid("currency must be NGN or USD");
  const category = input.category.trim();
  if (!category) throw new Invalid("category is required");
  const recurring = input.recurring || "once";
  if (!["once", "monthly", "yearly"].includes(recurring)) throw new Invalid("recurring must be once, monthly or yearly");

  return db.one(
    `INSERT INTO expenses (amount_minor, currency, category, description, spent_on, recurring, created_by)
     VALUES ($1, $2, $3, $4, COALESCE($5::date, current_date), $6, $7)
     RETURNING ${expenseColumns}`,
    [input.amount_minor, currency, category, input.description.trim(), input.spent_on || null, recurring, operatorId],
  );
}

export async function deleteExpense(db: Db, id: string) {
  await db.exec(`DELETE FROM expenses WHERE id = $1`, [id]);
}

async function spending(db: Db, limit: number) {
  return {
    // monthly_minor counts monthly items as they are and yearly ones divided by twelve.
    totals: await db.query(
      `SELECT currency, sum(amount_minor) AS total_minor,
              sum(CASE recurring WHEN 'monthly' THEN amount_minor WHEN 'yearly' THEN amount_minor / 12 ELSE 0 END) AS monthly_minor,
              count(*) AS count
         FROM expenses GROUP BY currency ORDER BY currency`,
    ),
    categories: await db.query(
      `SELECT category, currency, sum(amount_minor) AS total_minor, count(*) AS count
         FROM expenses GROUP BY category, currency ORDER BY sum(amount_minor) DESC LIMIT 12`,
    ),
    recent: await db.query(`SELECT ${expenseColumns} FROM expenses ORDER BY spent_on DESC, created_at DESC LIMIT $1`, [limit]),
  };
}

// --- operators -------------------------------------------------------------------

// roleFor resolves somebody's role: ENVI_ADMINS first, then the operators table.
export async function roleFor(db: Db, email: string, envAdmins: string[]): Promise<string> {
  const normalised = email.trim().toLowerCase();
  if (!normalised) return "";
  if (envAdmins.includes(normalised)) return ROLE_ADMIN;
  const row = await db.one(`SELECT role FROM operators WHERE email = $1`, [normalised]).catch(() => undefined);
  return row?.role ?? "";
}

// listOperators lists everyone, with the ENVI_ADMINS addresses first and marked.
export async function listOperators(db: Db, envAdmins: string[]) {
  const fromEnv = envAdmins.map((email) => ({
    id: "", email, name: "Deploy admin", role: ROLE_ADMIN, from_env: true, created_at: "0001-01-01T00:00:00Z",
  }));
  const rows = await db.query(`SELECT id, email, name, role, false AS from_env, created_at FROM operators ORDER BY role, created_at`);
  return [...fromEnv, ...rows.filter((o) => !envAdmins.includes(o.email))];
}

export async function addOperator(db: Db, actorId: string, rawEmail: string, rawName: string, role: string) {
  const email = rawEmail.trim().toLowerCase();
  if (!email || !email.includes("@") || /\s/.test(email)) throw new Invalid("a valid email address is required");
  const name = rawName.trim();
  if (!name) throw new Invalid("name is required");
  if (role !== ROLE_ADMIN && role !== ROLE_ANNOUNCER) throw new Invalid("role must be admin or announcer");
  return db.one(
    `INSERT INTO operators (email, name, role, created_by) VALUES ($1, $2, $3, $4)
         ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role
     RETURNING id, email, name, role, false AS from_env, created_at`,
    [email, name, role, actorId || null],
  );
}

export async function removeOperator(db: Db, id: string) {
  await db.exec(`DELETE FROM operators WHERE id = $1`, [id]);
}

// --- announcements -------------------------------------------------------------------

const announcementColumns = `id, subject, body, sent_by_email, recipients, failures, created_at`;

export async function recipients(db: Db): Promise<string[]> {
  const rows = await db.query(
    `SELECT DISTINCT u.email FROM users u
       JOIN memberships m ON m.user_id = u.id
       JOIN organizations o ON o.id = m.org_id
      WHERE o.is_subscribed
      ORDER BY u.email`,
  );
  return rows.map((r) => r.email);
}

export async function recipientCount(db: Db): Promise<number> {
  const row = await db.one(
    `SELECT count(DISTINCT u.email) AS n FROM users u
       JOIN memberships m ON m.user_id = u.id
       JOIN organizations o ON o.id = m.org_id
      WHERE o.is_subscribed`,
  );
  return row!.n;
}

export async function recordAnnouncement(db: Db, actorId: string, actorEmail: string, rawSubject: string, rawBody: string, count: number) {
  const subject = rawSubject.trim();
  if (!subject) throw new Invalid("subject is required");
  const body = rawBody.trim();
  if (!body) throw new Invalid("body is required");
  return db.one(
    `INSERT INTO announcements (subject, body, sent_by, sent_by_email, recipients)
     VALUES ($1, $2, $3, $4, $5) RETURNING ${announcementColumns}`,
    [subject, body, actorId || null, actorEmail, count],
  );
}

export async function noteFailures(db: Db, id: string, failures: number) {
  await db.exec(`UPDATE announcements SET failures = $2 WHERE id = $1`, [id, failures]);
}

export async function listAnnouncements(db: Db, limit: number) {
  return db.query(`SELECT ${announcementColumns} FROM announcements ORDER BY created_at DESC LIMIT $1`, [limit]);
}
