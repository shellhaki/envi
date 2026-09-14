import { canManage } from "./access";
import { assertLinkableWebUrl } from "./config";
import { all, blob, changed, now, one, run, uuid } from "./db";
import { badRequest, forbidden, tooMany } from "./errors";
import { hashToken, randomToken } from "./crypto";
import { invitationEmail, send } from "./mail";
import type { Env, Permission } from "./types";

const DEFAULT_TTL_MS = 7 * 24 * 60 * 60 * 1000;
const RATE_LIMIT = 20;
const RATE_WINDOW_MS = 60 * 60 * 1000;

export type Invitation = {
  ID: string;
  Token: string;
  Email: string;
  ProjectID: string;
  EnvironmentID: string;
  Permission: string;
  ExpiresAt: string;
};

export async function create(
  env: Env,
  userId: string,
  projectId: string,
  environmentId: string,
  rawEmail: string,
  permission: Permission,
  ttlSeconds: number,
): Promise<Invitation> {
  const email = rawEmail.trim().toLowerCase();
  if (!email || !email.includes("@")) throw badRequest("valid email required");
  if (!["read", "write", "manage"].includes(permission)) throw badRequest("invalid permission");
  if (!(await canManage(env.DB, projectId, userId, environmentId))) throw forbidden();
  assertLinkableWebUrl(env);

  const sent = await one<{ n: number }>(
    env.DB,
    `SELECT COUNT(*) AS n FROM invitations WHERE invited_by=? AND created_at > ?`,
    userId,
    now() - RATE_WINDOW_MS,
  );
  if ((sent?.n ?? 0) >= RATE_LIMIT) throw tooMany("too many invitations sent recently; try again later");

  const inviter = await one<{ email: string }>(env.DB, `SELECT email FROM users WHERE id=?`, userId);
  if (inviter && inviter.email.toLowerCase() === email) {
    throw badRequest("you already have access — you can't invite yourself");
  }

  const hasAccess = await one<{ ok: number }>(
    env.DB,
    `SELECT EXISTS(
       SELECT 1 FROM users u JOIN projects p ON p.id=?1 WHERE LOWER(u.email)=?2 AND (
         EXISTS(SELECT 1 FROM memberships m WHERE m.org_id=p.org_id AND m.user_id=u.id)
         OR EXISTS(SELECT 1 FROM access_grants g WHERE g.subject_user_id=u.id AND g.project_id=p.id
                   AND (g.environment_id IS NULL OR g.environment_id=?3)))
     ) AS ok`,
    projectId,
    email,
    environmentId || null,
  );
  if (hasAccess?.ok) throw badRequest("this person already has access to this project");

  const pending = await one<{ ok: number }>(
    env.DB,
    `SELECT EXISTS(SELECT 1 FROM invitations WHERE project_id=?1 AND email=?2 AND status='pending'
       AND expires_at>?3 AND environment_id IS ?4) AS ok`,
    projectId,
    email,
    now(),
    environmentId || null,
  );
  if (pending?.ok) throw badRequest("there's already a pending invitation for this address");

  const token = randomToken();
  const expiresAt = now() + (ttlSeconds > 0 ? ttlSeconds * 1000 : DEFAULT_TTL_MS);
  const id = uuid();
  await run(
    env.DB,
    `INSERT INTO invitations(id,project_id,environment_id,email,permission,token_hash,invited_by,status,expires_at,created_at)
     VALUES(?,?,?,?,?,?,?, 'pending',?,?)`,
    id,
    projectId,
    environmentId || null,
    email,
    permission,
    blob(await hashToken(token)),
    userId,
    expiresAt,
    now(),
  );

  await notify(env, email, projectId, permission, token, expiresAt);
  return {
    ID: id,
    Token: token,
    Email: email,
    ProjectID: projectId,
    EnvironmentID: environmentId,
    Permission: permission,
    ExpiresAt: new Date(expiresAt).toISOString(),
  };
}

async function notify(env: Env, email: string, projectId: string, permission: string, token: string, expiresAt: number) {
  const project = await one<{ name: string }>(env.DB, `SELECT name FROM projects WHERE id=?`, projectId);
  const link = `${env.ENVI_WEB_URL.replace(/\/+$/, "")}/invite/${token}`;
  const expires = new Date(expiresAt).toUTCString();
  const msg = invitationEmail(project?.name ?? "a project", permission, link, expires);
  // Delivery failure must not fail the invitation: it exists, and the token is
  // returned to the caller as a fallback.
  try {
    await send(env, { ...msg, to: email });
  } catch (err) {
    console.error("invitation email delivery failed:", err);
  }
}

export async function preview(db: D1Database, token: string) {
  const row = await one<{
    email: string;
    project: string;
    environment: string | null;
    permission: string;
    expires_at: number;
  }>(
    db,
    `SELECT i.email AS email, p.name AS project, e.name AS environment, i.permission AS permission, i.expires_at AS expires_at
     FROM invitations i JOIN projects p ON p.id=i.project_id
     LEFT JOIN environments e ON e.id=i.environment_id
     WHERE i.token_hash=? AND i.status='pending' AND i.expires_at>?`,
    blob(await hashToken(token)),
    now(),
  );
  if (!row) throw forbidden();
  return {
    Email: row.email,
    ProjectName: row.project,
    EnvironmentName: row.environment ?? "",
    Permission: row.permission,
    ExpiresAt: new Date(row.expires_at).toISOString(),
  };
}

export async function accept(db: D1Database, userId: string, token: string): Promise<void> {
  const row = await one<{ id: string; project_id: string; environment_id: string | null; permission: string }>(
    db,
    `SELECT i.id, i.project_id, i.environment_id, i.permission FROM invitations i
     JOIN users u ON LOWER(u.email)=LOWER(i.email)
     WHERE i.token_hash=? AND u.id=? AND i.status='pending' AND i.expires_at>?`,
    blob(await hashToken(token)),
    userId,
    now(),
  );
  if (!row) throw forbidden();

  const claimed = await changed(
    db,
    `UPDATE invitations SET status='accepted' WHERE id=? AND status='pending'`,
    row.id,
  );
  if (claimed !== 1) throw forbidden();

  await run(
    db,
    `INSERT OR IGNORE INTO access_grants(id,subject_user_id,project_id,environment_id,permission,created_at)
     VALUES(?,?,?,?,?,?)`,
    uuid(),
    userId,
    row.project_id,
    row.environment_id,
    row.permission,
    now(),
  );
}

export type Collaborator = {
  id: string;
  email: string;
  permission: string;
  environment_id?: string;
  status: "active" | "pending";
  expires_at?: string;
};

export async function listCollaborators(db: D1Database, userId: string, projectId: string): Promise<Collaborator[]> {
  if (!(await canManage(db, projectId, userId))) throw forbidden();

  const grants = await all<{ id: string; email: string; permission: string; environment_id: string | null }>(
    db,
    `SELECT g.id, u.email, g.permission, g.environment_id FROM access_grants g
     JOIN users u ON u.id=g.subject_user_id WHERE g.project_id=? ORDER BY g.created_at DESC`,
    projectId,
  );
  const pending = await all<{ id: string; email: string; permission: string; environment_id: string | null; expires_at: number }>(
    db,
    `SELECT id,email,permission,environment_id,expires_at FROM invitations
     WHERE project_id=? AND status='pending' AND expires_at>? ORDER BY created_at DESC`,
    projectId,
    now(),
  );

  return [
    ...grants.map((g) => ({
      id: g.id,
      email: g.email,
      permission: g.permission,
      ...(g.environment_id ? { environment_id: g.environment_id } : {}),
      status: "active" as const,
    })),
    ...pending.map((p) => ({
      id: p.id,
      email: p.email,
      permission: p.permission,
      ...(p.environment_id ? { environment_id: p.environment_id } : {}),
      status: "pending" as const,
      expires_at: new Date(p.expires_at).toISOString(),
    })),
  ];
}

export async function revokeInvitation(db: D1Database, userId: string, projectId: string, id: string): Promise<void> {
  if (!(await canManage(db, projectId, userId))) throw forbidden();
  const count = await changed(
    db,
    `UPDATE invitations SET status='revoked' WHERE id=? AND project_id=? AND status='pending'`,
    id,
    projectId,
  );
  if (count === 0) throw forbidden();
}

/** Only ever touches access_grants, so an owner's membership cannot be revoked here. */
export async function revokeGrant(db: D1Database, userId: string, projectId: string, id: string): Promise<void> {
  if (!(await canManage(db, projectId, userId))) throw forbidden();
  const count = await changed(db, `DELETE FROM access_grants WHERE id=? AND project_id=?`, id, projectId);
  if (count === 0) throw forbidden();
}
