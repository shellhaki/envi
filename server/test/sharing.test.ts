import { describe, expect, test } from "bun:test";
import { call, newProject, newUser, testDeps } from "./helpers";

describe("invitations", () => {
  test("an invited person accepts and gets access", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps, "owner");
    const guest = await newUser(deps, "guest");
    const { project, env } = await newProject(deps, owner, "shared");
    await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: owner.token, body: { K: "v" } });

    const invited = await call(deps, `/projects/${project.ID}/invitations`, {
      method: "POST", token: owner.token, body: { email: guest.email.toUpperCase(), permission: "write" },
    });
    expect(invited.status).toBe(201);
    expect(invited.json).toMatchObject({ Email: guest.email, ProjectID: project.ID, EnvironmentID: "", Permission: "write" });

    const email = deps.mailer.sent.findLast((m) => m.to === guest.email)!;
    expect(email.text).toContain(invited.json.Token);

    const preview = await call(deps, `/invitations/${invited.json.Token}`);
    expect(preview.json).toEqual({ Email: guest.email, ProjectName: "shared", EnvironmentName: "", Permission: "write", ExpiresAt: expect.any(String) });

    expect((await call(deps, "/invitations/accept", { method: "POST", token: guest.token, body: { token: invited.json.Token } })).status).toBe(204);
    expect((await call(deps, "/projects", { token: guest.token })).json).toEqual([project]);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token: guest.token })).json).toEqual({ K: "v" });
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: guest.token, body: { G: "1" } })).status).toBe(204);

    expect((await call(deps, `/invitations/${invited.json.Token}`)).status).toBe(404);
    expect((await call(deps, "/invitations/accept", { method: "POST", token: guest.token, body: { token: invited.json.Token } })).status).toBe(403);
  });

  test("only the invited email can accept", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const other = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const invited = await call(deps, `/projects/${project.ID}/invitations`, { method: "POST", token: owner.token, body: { email: "someone@example.com" } });
    expect((await call(deps, "/invitations/accept", { method: "POST", token: other.token, body: { token: invited.json.Token } })).status).toBe(403);
  });

  test("an environment invite hides other environments", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const guest = await newUser(deps);
    const { project, env } = await newProject(deps, owner);
    const prod = await call(deps, `/projects/${project.ID}/environments`, { method: "POST", token: owner.token, body: { name: "prod", is_production: true } });
    const invited = await call(deps, `/projects/${project.ID}/invitations`, {
      method: "POST", token: owner.token, body: { email: guest.email, environment_id: env.ID },
    });
    await call(deps, "/invitations/accept", { method: "POST", token: guest.token, body: { token: invited.json.Token } });

    const envs = await call(deps, `/projects/${project.ID}/environments`, { token: guest.token });
    expect(envs.json.map((e: any) => e.Name)).toEqual(["default"]);
    expect((await call(deps, `/environments/${prod.json.ID}/secrets`, { token: guest.token })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: guest.token, body: { A: "1" } })).status).toBe(403);
  });

  test("bad invitations are refused", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const stranger = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const invite = (token: string, body: object) => call(deps, `/projects/${project.ID}/invitations`, { method: "POST", token, body });

    expect((await invite(owner.token, {})).status).toBe(400);
    expect((await invite(owner.token, { email: owner.email })).json.error).toContain("can't invite yourself");
    expect((await invite(owner.token, { email: "x@example.com", permission: "root" })).status).toBe(400);
    expect((await invite(owner.token, { email: "x@example.com", environment_id: crypto.randomUUID() })).status).toBe(403);
    expect((await invite(stranger.token, { email: "x@example.com" })).status).toBe(403);

    expect((await invite(owner.token, { email: "twice@example.com" })).status).toBe(201);
    expect((await invite(owner.token, { email: "twice@example.com" })).json.error).toContain("already a pending invitation");
    expect((await call(deps, "/invitations/not-a-token")).status).toBe(404);
  });

  test("collaborators are listed, revoked and removed", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const guest = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const accepted = await call(deps, `/projects/${project.ID}/invitations`, { method: "POST", token: owner.token, body: { email: guest.email } });
    await call(deps, "/invitations/accept", { method: "POST", token: guest.token, body: { token: accepted.json.Token } });
    const pending = await call(deps, `/projects/${project.ID}/invitations`, { method: "POST", token: owner.token, body: { email: "later@example.com" } });

    const list = await call(deps, `/projects/${project.ID}/collaborators`, { token: owner.token });
    expect(list.json).toEqual([
      { id: expect.any(String), email: guest.email, permission: "read", status: "active" },
      { id: pending.json.ID, email: "later@example.com", permission: "read", status: "pending", expires_at: expect.any(String) },
    ]);
    expect((await call(deps, `/projects/${project.ID}/collaborators`, { token: guest.token })).status).toBe(403);

    expect((await call(deps, `/projects/${project.ID}/invitations/${pending.json.ID}`, { method: "DELETE", token: owner.token })).status).toBe(204);
    expect((await call(deps, `/invitations/${pending.json.Token}`)).status).toBe(404);

    const grantId = list.json[0].id;
    expect((await call(deps, `/projects/${project.ID}/collaborators/${grantId}`, { method: "DELETE", token: owner.token })).status).toBe(204);
    expect((await call(deps, "/projects", { token: guest.token })).json).toEqual([]);
    expect((await call(deps, `/projects/${project.ID}/collaborators/${grantId}`, { method: "DELETE", token: owner.token })).status).toBe(403);
  });

  test("a write api key may not invite", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const key = await call(deps, "/me/api-keys", { method: "POST", token: owner.token, body: { name: "w", permission: "write" } });
    const session = await call(deps, "/auth/api-key", { method: "POST", body: { key: key.json.secret } });
    const reply = await call(deps, `/projects/${project.ID}/invitations`, { method: "POST", token: session.json.access_token, body: { email: "x@example.com" } });
    expect(reply.status).toBe(403);
    expect(reply.json.code).toBe("insufficient_permission");
  });
});

describe("audit log", () => {
  test("members see their organization's events", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const stranger = await newUser(deps);
    const { env } = await newProject(deps, owner);
    await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: owner.token, body: { A: "1" } });
    const events = await call(deps, `/orgs/${owner.orgId}/audit-events`, { token: owner.token });
    expect(events.status).toBe(200);
    expect(events.json.length).toBeGreaterThan(0);
    expect((await call(deps, `/orgs/${owner.orgId}/audit-events`, { token: stranger.token })).status).toBe(403);
  });
});
