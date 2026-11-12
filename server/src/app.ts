import { Hono } from "hono";
import type { AppEnv } from "./deps";
import { canAnnounce, canManage, operator, signedIn } from "./middleware";
import * as account from "./routes/account";
import * as admin from "./routes/admin";
import * as auth from "./routes/auth";
import * as billing from "./routes/billing";
import * as projects from "./routes/projects";
import * as secrets from "./routes/secrets";
import * as sharing from "./routes/sharing";

// Every route, in one place. Handlers get their dependencies from c.env.deps.
export const app = new Hono<AppEnv>();

app.get("/health", (c) => c.json({ status: "ok" }));

// Sign-in
app.post("/auth/request-otp", auth.requestLoginCode);
app.post("/auth/verify-otp", auth.verifyLoginCode);
app.post("/auth/refresh", auth.refresh);
app.post("/auth/logout", auth.logout);
app.post("/auth/api-key", auth.exchangeApiKey);

// CLI sign-in through the browser
app.post("/auth/device/code", auth.startDevice);
app.post("/auth/device/token", auth.finishDevice);
app.post("/auth/device/approve", signedIn, auth.approveDevice);
app.post("/auth/device/deny", signedIn, auth.denyDevice);

// Account and API keys
app.get("/me", signedIn, account.me);
app.post("/me/api-keys", signedIn, account.createApiKey);
app.get("/me/api-keys", signedIn, account.listApiKeys);
app.delete("/me/api-keys/:id", signedIn, account.revokeApiKey);

// Projects and environments
app.post("/projects", signedIn, projects.createProject);
app.get("/projects", signedIn, projects.listProjects);
app.delete("/projects/:id", signedIn, projects.deleteProject);
app.post("/projects/:id/environments", signedIn, projects.createEnvironment);
app.get("/projects/:id/environments", signedIn, projects.listEnvironments);
app.patch("/environments/:id", signedIn, projects.updateEnvironment);
app.delete("/environments/:id", signedIn, projects.deleteEnvironment);

// Secrets
app.get("/environments/:id/secrets", signedIn, secrets.getSecrets);
app.get("/environments/:id/secrets/snapshot", signedIn, secrets.getSnapshot);
app.put("/environments/:id/secrets", signedIn, secrets.putSecrets);
app.put("/environments/:id/secrets/snapshot", signedIn, secrets.putSnapshot);
app.delete("/environments/:id/secrets/:key", signedIn, secrets.deleteSecret);
app.get("/values", signedIn, secrets.getValues);

// Key-value store
app.get("/kv", signedIn, secrets.listKv);
app.put("/kv", signedIn, secrets.setKv);
app.delete("/kv", signedIn, secrets.deleteKv);

// Service tokens
app.post("/projects/:id/environments/:env/service-tokens", signedIn, sharing.createServiceToken);
app.delete("/service-tokens", signedIn, sharing.revokeServiceToken);

// Invitations and collaborators
app.get("/invitations/:token", sharing.previewInvitation);
app.post("/projects/:id/invitations", signedIn, sharing.createInvitation);
app.post("/invitations/accept", signedIn, sharing.acceptInvitation);
app.get("/projects/:id/collaborators", signedIn, sharing.listCollaborators);
app.delete("/projects/:id/invitations/:invitationID", signedIn, sharing.revokeInvitation);
app.delete("/projects/:id/collaborators/:grantID", signedIn, sharing.removeCollaborator);

// Audit log
app.get("/orgs/:id/audit-events", signedIn, account.listAuditEvents);

// Billing (the caller's own organization)
app.get("/billing", signedIn, billing.overview);
app.post("/billing/payg", signedIn, billing.startPayg);
app.post("/billing/team", signedIn, billing.startTeam);
app.post("/billing/cancel", signedIn, billing.cancel);
app.post("/billing/portal", signedIn, billing.portal);
app.post("/billing/retry", signedIn, billing.retry);
app.post("/webhooks/bachs", billing.webhook);

// Operators (answers 404 to everyone else)
app.get("/admin/whoami", signedIn, operator, admin.whoami);
app.get("/admin/stats", signedIn, operator, canManage, admin.stats);
app.post("/admin/expenses", signedIn, operator, canManage, admin.addExpense);
app.delete("/admin/expenses/:id", signedIn, operator, canManage, admin.deleteExpense);
app.get("/admin/operators", signedIn, operator, canManage, admin.listOperators);
app.post("/admin/operators", signedIn, operator, canManage, admin.addOperator);
app.delete("/admin/operators/:id", signedIn, operator, canManage, admin.removeOperator);
app.get("/admin/announcements", signedIn, operator, canAnnounce, admin.listAnnouncements);
app.post("/admin/announcements", signedIn, operator, canAnnounce, admin.sendAnnouncement);

app.notFound((c) => c.text("404 page not found", 404));

app.onError((err, c) => {
  console.error(`${c.req.method} ${c.req.path}: ${err.stack ?? err.message}`);
  return c.json({ code: "internal", error: "internal error" }, 500);
});
