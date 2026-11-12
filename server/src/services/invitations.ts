// Invitations to a project or one environment of it, and the collaborators they create.

import { hashToken, randomToken } from "../crypto";
import type { Deps } from "../deps";
import type { Queryable } from "../db";
import { invitationEmail } from "../emails/invitation";
import { Forbidden, Invalid, RateLimited } from "../errors";
import * as billing from "./billing";

const INVITATIONS_PER_HOUR = 20;
const DEFAULT_TTL_SECONDS = 7 * 24 * 60 * 60;

// Field names match what the dashboard and CLI already read.
export type Invitation = {
  ID: string;
  Token: string;
  Email: string;
  ProjectID: string;
  EnvironmentID: string;
  Permission: string;
  ExpiresAt: Date;
};

export type Preview = { Email: string; ProjectName: string; EnvironmentName: string; Permission: string; ExpiresAt: Date };

export type Collaborator = {
  id: string;
  email: string;
  permission: string;
  environment_id?: string;
  status: "active" | "pending";
  expires_at?: Date;
};

// createInvitation invites an email to a project (or one environment of it) and emails the link.
export async function createInvitation(
  deps: Deps, userId: string, projectId: string, envId: string, rawEmail: string, permission: string, ttlSeconds: number,
): Promise<Invitation> {
  const { db } = deps;
  const email = rawEmail.trim().toLowerCase();
  if (!email || !email.includes("@")) throw new Invalid("valid email required");
  if (!["read", "write", "manage"].includes(permission)) throw new Invalid("invalid permission");
  if (!(await canManage(db, projectId, userId, envId))) throw new Forbidden();

  const sent = await db.one(
    `SELECT count(*) AS n FROM invitations WHERE invited_by = $1 AND created_at > now() - interval '1 hour'`,
    [userId],
  );
  if (sent!.n >= INVITATIONS_PER_HOUR) throw new RateLimited("too many invitations sent recently; try again later");

  const inviter = await db.one(`SELECT email FROM users WHERE id = $1`, [userId]);
  if (inviter && inviter.email.toLowerCase() === email) throw new Invalid("you already have access — you can't invite yourself");

  const access = await db.one(
    `SELECT EXISTS (
       SELECT 1 FROM users u JOIN projects p ON p.id = $1
        WHERE lower(u.email) = $2
          AND (EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = p.org_id AND m.user_id = u.id)
            OR EXISTS (SELECT 1 FROM access_grants g
                        WHERE g.subject_user_id = u.id AND g.project_id = p.id
                          AND (g.environment_id IS NULL OR g.environment_id = NULLIF($3, '')::uuid)))
     ) AS ok`,
    [projectId, email, envId],
  );
  if (access!.ok) throw new Invalid("this person already has access to this project");

  const pending = await db.one(
    `SELECT EXISTS (
       SELECT 1 FROM invitations
        WHERE project_id = $1 AND email = $2 AND status = 'pending' AND expires_at > now()
          AND environment_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
     ) AS ok`,
    [projectId, email, envId],
  );
  if (pending!.ok) throw new Invalid("there's already a pending invitation for this address");

  if (deps.bachs) await billing.allowNewPerson(deps, await orgOf(db, projectId), email);

  const token = randomToken();
  const ttl = ttlSeconds > 0 ? ttlSeconds : DEFAULT_TTL_SECONDS;
  const row = await db.one(
    `INSERT INTO invitations (project_id, environment_id, email, permission, token_hash, invited_by, expires_at)
     VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, now() + make_interval(secs => $7))
     RETURNING id, expires_at`,
    [projectId, envId, email, permission, await hashToken(token), userId, ttl],
  );
  const invitation: Invitation = {
    ID: row!.id, Token: token, Email: email, ProjectID: projectId, EnvironmentID: envId, Permission: permission, ExpiresAt: row!.expires_at,
  };
  await sendInvitationEmail(deps, invitation);
  return invitation;
}

// previewInvitation returns a pending invitation for the accept page. It needs no sign-in.
export async function previewInvitation(deps: Deps, token: string): Promise<Preview> {
  const row = await deps.db.one<Preview>(
    `SELECT i.email AS "Email", pr.name AS "ProjectName", COALESCE(e.name, '') AS "EnvironmentName",
            i.permission AS "Permission", i.expires_at AS "ExpiresAt"
       FROM invitations i
       JOIN projects pr ON pr.id = i.project_id
       LEFT JOIN environments e ON e.id = i.environment_id
      WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()`,
    [await hashToken(token)],
  );
  if (!row) throw new Forbidden();
  return row;
}

// acceptInvitation turns an invitation into an access grant. The signed-in user's email must match it.
export async function acceptInvitation(deps: Deps, userId: string, token: string): Promise<void> {
  const hash = await hashToken(token);
  const org = await deps.db.transaction(async (tx) => {
    const invitation = await tx.one(
      `SELECT i.id, i.project_id, i.environment_id, i.permission
         FROM invitations i JOIN users u ON lower(u.email) = lower(i.email)
        WHERE i.token_hash = $1 AND u.id = $2 AND i.status = 'pending' AND i.expires_at > now()
          FOR UPDATE OF i`,
      [hash, userId],
    );
    if (!invitation) throw new Forbidden();

    // Checked again here, or several invitations sent below the limit could all be accepted past it.
    let orgId = "";
    if (deps.bachs) {
      orgId = await orgOf(tx, invitation.project_id);
      const user = await tx.one(`SELECT email FROM users WHERE id = $1`, [userId]);
      await billing.allowNewPerson(deps, orgId, user!.email);
    }
    await tx.exec(
      `INSERT INTO access_grants (subject_user_id, project_id, environment_id, permission)
       VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
      [userId, invitation.project_id, invitation.environment_id, invitation.permission],
    );
    await tx.exec(`UPDATE invitations SET status = 'accepted' WHERE id = $1`, [invitation.id]);
    return orgId;
  });

  if (deps.bachs) {
    await billing.recordPerson(deps, org, userId).catch((err) => {
      console.error(`billing: recording ${userId} in ${org}: ${err.message}`); // the hourly sweep catches it anyway
    });
  }
}

// listCollaborators returns everyone with access to a project, then open invitations.
export async function listCollaborators(deps: Deps, userId: string, projectId: string): Promise<Collaborator[]> {
  if (!(await canManage(deps.db, projectId, userId, ""))) throw new Forbidden();
  const grants = await deps.db.query(
    `SELECT g.id::text AS id, u.email, g.permission, COALESCE(g.environment_id::text, '') AS environment_id
       FROM access_grants g JOIN users u ON u.id = g.subject_user_id
      WHERE g.project_id = $1
      ORDER BY g.created_at DESC`,
    [projectId],
  );
  const pending = await deps.db.query(
    `SELECT id::text AS id, email, permission, COALESCE(environment_id::text, '') AS environment_id, expires_at
       FROM invitations
      WHERE project_id = $1 AND status = 'pending' AND expires_at > now()
      ORDER BY created_at DESC`,
    [projectId],
  );
  return [
    ...grants.map((g) => collaborator(g, "active")),
    ...pending.map((p) => collaborator(p, "pending")),
  ];
}

export async function revokeInvitation(deps: Deps, userId: string, projectId: string, invitationId: string): Promise<void> {
  await manageExec(deps, userId, projectId,
    `UPDATE invitations SET status = 'revoked' WHERE id = $1 AND project_id = $2 AND status = 'pending'`, invitationId);
}

// removeCollaborator removes someone's access. Owners have memberships, not grants, so they can't be removed here.
export async function removeCollaborator(deps: Deps, userId: string, projectId: string, grantId: string): Promise<void> {
  await manageExec(deps, userId, projectId, `DELETE FROM access_grants WHERE id = $1 AND project_id = $2`, grantId);
}

async function manageExec(deps: Deps, userId: string, projectId: string, statement: string, id: string) {
  if (!(await canManage(deps.db, projectId, userId, ""))) throw new Forbidden();
  if ((await deps.db.exec(statement, [id, projectId])) === 0) throw new Forbidden();
}

// canManage: an org owner or admin, or anyone with a manage grant. A non-empty envId must belong to the project.
async function canManage(db: Queryable, projectId: string, userId: string, envId: string): Promise<boolean> {
  const row = await db
    .one(
      `SELECT EXISTS (
         SELECT 1 FROM projects p
          WHERE p.id = $1
            AND (EXISTS (SELECT 1 FROM memberships m WHERE m.org_id = p.org_id AND m.user_id = $2 AND m.role IN ('owner', 'admin'))
              OR EXISTS (SELECT 1 FROM access_grants g WHERE g.project_id = p.id AND g.subject_user_id = $2 AND g.permission = 'manage'))
            AND (NULLIF($3::text, '') IS NULL
              OR EXISTS (SELECT 1 FROM environments e WHERE e.id = NULLIF($3::text, '')::uuid AND e.project_id = p.id))
       ) AS ok`,
      [projectId, userId || null, envId],
    )
    .catch(() => undefined);
  return Boolean(row?.ok);
}

// orgOf returns the organization that owns a project, which is who pays for its collaborators.
async function orgOf(db: Queryable, projectId: string): Promise<string> {
  const row = await db.one(`SELECT org_id FROM projects WHERE id = $1`, [projectId]);
  return row!.org_id;
}

function collaborator(row: Record<string, any>, status: "active" | "pending"): Collaborator {
  const c: Collaborator = { id: row.id, email: row.email, permission: row.permission, status };
  if (row.environment_id) c.environment_id = row.environment_id;
  if (row.expires_at) c.expires_at = row.expires_at;
  return c;
}

async function sendInvitationEmail(deps: Deps, invitation: Invitation) {
  const project = await deps.db.one(`SELECT name FROM projects WHERE id = $1`, [invitation.ProjectID]).catch(() => undefined);
  const webUrl = deps.config.webUrl;
  if (webUrl.includes("localhost") || webUrl.includes("127.0.0.1")) {
    console.warn(`WARNING: emailing ${invitation.Email} an invitation link on ${webUrl} — it will not resolve for them. Set ENVI_WEB_URL to the public dashboard URL.`);
  }
  try {
    await deps.mailer.send(invitationEmail(invitation, project?.name || "a project", webUrl));
  } catch (err) {
    console.error(`invitation email delivery failed: ${(err as Error).message}`); // the invitation exists either way
  }
}
