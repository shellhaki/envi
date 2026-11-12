// Accounts, meaning the user and personal organization made on first sign-in.

import type { Deps } from "../deps";
import { welcomeEmail } from "../emails/welcome";

export type Personal = { userId: string; organizationId: string; created: boolean };

// provision finds or creates the user and their personal organization.
export async function provision(deps: Deps, rawEmail: string): Promise<Personal> {
  const email = rawEmail.trim().toLowerCase();
  if (!email) throw new Error("email is required");

  const result = await deps.db.transaction(async (tx) => {
    // Two sign-ins for the same email at once would otherwise create two organizations.
    await tx.query(`SELECT pg_advisory_xact_lock(hashtext($1))`, [email]);
    // xmax = 0 is true when this statement inserted the row, false when it updated an existing one.
    const user = await tx.one(
      `INSERT INTO users (email) VALUES ($1)
           ON CONFLICT (email) DO UPDATE SET email = excluded.email
       RETURNING id, (xmax = 0) AS created`,
      [email],
    );
    let org = await tx.one(
      `SELECT o.id FROM organizations o JOIN memberships m ON m.org_id = o.id
        WHERE m.user_id = $1 AND o.type = 'personal'`,
      [user!.id],
    );
    if (!org) {
      org = await tx.one(`INSERT INTO organizations (name, type) VALUES ($1, 'personal') RETURNING id`, [email]);
      await tx.exec(`INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, 'owner')`, [user!.id, org!.id]);
    }
    return { userId: user!.id as string, organizationId: org!.id as string, created: user!.created as boolean };
  });

  if (result.created) {
    deps.background(
      deps.mailer.send(welcomeEmail(email, deps.config.webUrl, deps.config.docsUrl)).catch((err) => {
        console.error(`welcome email to ${email} failed: ${err.message}`);
      }),
    );
  }
  return result;
}

export async function findOrCreateAccount(deps: Deps, email: string): Promise<string> {
  return (await provision(deps, email)).userId;
}
