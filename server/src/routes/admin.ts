import { Invalid } from "../errors";
import { fail, readBody, param, type Ctx } from "../http";
import * as admin from "../services/admin";
import { sendAnnouncement as send } from "../services/announce";

// GET /admin/whoami tells the dashboard whether to show the admin link.
export function whoami(c: Ctx) {
  const role = c.get("operatorRole");
  return c.json({
    operator: true,
    role,
    email: c.get("operatorEmail"),
    announce: admin.mayAnnounce(role),
    manage: admin.mayManage(role),
  });
}

// GET /admin/stats
export async function stats(c: Ctx) {
  const { db, config } = c.env.deps;
  try {
    return c.json(await admin.snapshot(db, config.mode, config.currency));
  } catch (err) {
    return fail(c, 500, "admin_stats_failed", (err as Error).message);
  }
}

// POST /admin/expenses {amount_minor, currency, category, description, spent_on, recurring}
export async function addExpense(c: Ctx) {
  const body = await readBody(c, {
    amount_minor: "number", currency: "string", category: "string", description: "string", spent_on: "string", recurring: "string",
  });
  if (!body) return fail(c, 400, "invalid_body", "invalid request body");
  if (body.spent_on && !isDate(body.spent_on)) return fail(c, 400, "invalid_date", "spent_on must be YYYY-MM-DD");
  try {
    return c.json(await admin.addExpense(c.env.deps.db, c.get("userId"), body), 201);
  } catch (err) {
    return fail(c, 400, "invalid_expense", (err as Error).message);
  }
}

// DELETE /admin/expenses/:id
export async function deleteExpense(c: Ctx) {
  try {
    await admin.deleteExpense(c.env.deps.db, param(c, "id"));
  } catch (err) {
    return fail(c, 500, "delete_failed", (err as Error).message);
  }
  return c.body(null, 204);
}

// GET /admin/operators
export async function listOperators(c: Ctx) {
  const { db, config } = c.env.deps;
  try {
    return c.json({ operators: await admin.listOperators(db, config.admins) });
  } catch (err) {
    return fail(c, 500, "operators_failed", (err as Error).message);
  }
}

// POST /admin/operators {email, name, role}
export async function addOperator(c: Ctx) {
  const body = await readBody(c, { email: "string", name: "string", role: "string" });
  if (!body) return fail(c, 400, "invalid_body", "invalid request body");
  try {
    return c.json(await admin.addOperator(c.env.deps.db, c.get("userId"), body.email, body.name, body.role), 201);
  } catch (err) {
    return fail(c, 400, "invalid_operator", (err as Error).message);
  }
}

// DELETE /admin/operators/:id
export async function removeOperator(c: Ctx) {
  try {
    await admin.removeOperator(c.env.deps.db, param(c, "id"));
  } catch (err) {
    return fail(c, 500, "delete_failed", (err as Error).message);
  }
  return c.body(null, 204);
}

// GET /admin/announcements
export async function listAnnouncements(c: Ctx) {
  const { db } = c.env.deps;
  try {
    return c.json({ announcements: await admin.listAnnouncements(db, 25), recipients: await admin.recipientCount(db) });
  } catch (err) {
    return fail(c, 500, "announcements_failed", (err as Error).message);
  }
}

// POST /admin/announcements {subject, body, confirm: true} emails every subscribed user.
export async function sendAnnouncement(c: Ctx) {
  const body = await readBody(c, { subject: "string", body: "string", confirm: "boolean" });
  if (!body) return fail(c, 400, "invalid_body", "invalid request body");
  if (!body.confirm) return fail(c, 400, "not_confirmed", "sending to everyone has to be confirmed");
  try {
    const sent = await send(c.env.deps, c.get("userId"), c.get("operatorEmail"), body.subject, body.body);
    return c.json(sent, 201);
  } catch (err) {
    if (err instanceof Invalid) return fail(c, 400, "announce_failed", err.message);
    return fail(c, 400, "announce_failed", (err as Error).message);
  }
}

function isDate(value: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && !Number.isNaN(Date.parse(value));
}
