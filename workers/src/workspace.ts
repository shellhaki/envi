import { now, one, run, uuid } from "./db";

export type Personal = { userId: string; organizationId: string };

/**
 * Finds or creates the user and their personal org. Postgres used an advisory
 * lock to serialise concurrent sign-ins for the same address; here the unique
 * index on users.email does that job — a loser re-reads the winner's row.
 */
export async function provision(db: D1Database, rawEmail: string): Promise<Personal> {
  const email = rawEmail.trim().toLowerCase();
  if (!email) throw new Error("email required");

  let user = await one<{ id: string }>(db, `SELECT id FROM users WHERE email=?`, email);
  if (!user) {
    const id = uuid();
    await run(db, `INSERT OR IGNORE INTO users(id,email,created_at) VALUES(?,?,?)`, id, email, now());
    user = await one<{ id: string }>(db, `SELECT id FROM users WHERE email=?`, email);
    if (!user) throw new Error("could not provision user");
  }

  const existing = await one<{ id: string }>(
    db,
    `SELECT o.id FROM organizations o JOIN memberships m ON m.org_id=o.id
     WHERE m.user_id=? AND o.type='personal'`,
    user.id,
  );
  if (existing) return { userId: user.id, organizationId: existing.id };

  const orgId = uuid();
  const t = now();
  await db.batch([
    db.prepare(`INSERT INTO organizations(id,name,type,created_at) VALUES(?,?, 'personal',?)`).bind(orgId, email, t),
    db
      .prepare(`INSERT INTO memberships(id,user_id,org_id,role,created_at) VALUES(?,?,?, 'owner',?)`)
      .bind(uuid(), user.id, orgId, t),
  ]);
  return { userId: user.id, organizationId: orgId };
}
