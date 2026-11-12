import { describe, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { usersFile } from "../src/services/instance";
import { syncUsers } from "../src/services/members";
import { call, newProject, newUser, testDeps } from "./helpers";

let n = 0;

// instance builds deps for a self-hosted instance whose users file names one admin.
async function instance() {
  const path = join(mkdtempSync(join(tmpdir(), "envi-users-")), "users.toml");
  const admin = `admin-${Date.now()}-${n++}@example.com`;
  writeFileSync(path, `[[user]]\nemail = "${admin}"\nrole = "admin"\npassword = "admin password 1"\n`);
  const deps = await testDeps({ usersFile: path });
  deps.users = usersFile(path);
  await syncUsers(deps);
  const signIn = (email: string, password: string) => call(deps, "/auth/password", { method: "POST", body: { email, password } });
  const token = (await signIn(admin, "admin password 1")).json.access_token as string;
  return { deps, path, admin, token, signIn };
}

describe("password sign-in", () => {
  test("a plain-text password in the file is hashed and the line rewritten", async () => {
    const { path, admin, signIn } = await instance();
    const text = readFileSync(path, "utf8");
    expect(text).not.toContain("admin password 1");
    expect(text).toContain('hash = "$2');
    expect(text).toContain("Forgot a password?");
    expect((await signIn(admin.toUpperCase(), "admin password 1")).status).toBe(200);
  });

  test("a reset written into the file works on the next sign-in", async () => {
    const { path, admin, signIn } = await instance();
    const text = readFileSync(path, "utf8").replace(/hash = ".*"/, 'password = "reset by hand 22"');
    writeFileSync(path, text);
    expect((await signIn(admin, "admin password 1")).status).toBe(401);
    expect((await signIn(admin, "reset by hand 22")).status).toBe(200);
  });

  test("wrong passwords are refused, then rate limited", async () => {
    const { admin, signIn } = await instance();
    const wrong = await signIn(admin, "nope");
    expect(wrong.status).toBe(401);
    expect(wrong.json.code).toBe("invalid_credentials");
    expect((await signIn("stranger@example.com", "x")).status).toBe(401);
    for (let i = 0; i < 9; i++) await signIn(admin, "nope");
    expect((await signIn(admin, "admin password 1")).status).toBe(429);
  });

  test("the instance says how it signs in, and email codes are off", async () => {
    const { deps } = await instance();
    expect((await call(deps, "/instance")).json).toEqual({ mode: "self-hosted", sign_in: "password", billing: false });
    expect((await call(deps, "/auth/request-otp", { method: "POST", body: { email: "a@b.co" } })).json.code).toBe("password_sign_in");
    const emailDeps = await testDeps();
    expect((await call(emailDeps, "/instance")).json.sign_in).toBe("email");
    expect((await call(emailDeps, "/auth/password", { method: "POST", body: { email: "a@b.co", password: "x" } })).status).toBe(404);
  });

  test("a users file with no admin stops the server starting", async () => {
    const { deps, path } = await instance();
    writeFileSync(path, `[[user]]\nemail = "only@example.com"\nrole = "contributor"\nhash = ""\n`);
    await expect(syncUsers(deps)).rejects.toThrow("has no admin");
  });
});

describe("the admin manages users", () => {
  test("adding a user returns a password they can sign in with", async () => {
    const { deps, token, signIn } = await instance();
    const added = await call(deps, "/instance/users", { method: "POST", token, body: { email: "Dev@Example.com" } });
    expect(added.status).toBe(201);
    expect(added.json).toEqual({ id: expect.any(String), email: "dev@example.com", password: expect.stringMatching(/^[a-z2-9]{4}(-[a-z2-9]{4}){3}$/) });

    const devToken = (await signIn("dev@example.com", added.json.password)).json.access_token;
    expect((await call(deps, "/me", { token: devToken })).json).toMatchObject({ Email: "dev@example.com", Role: "contributor" });
    expect((await call(deps, "/me", { token })).json.Role).toBe("admin");

    expect((await call(deps, "/instance/users", { method: "POST", token, body: { email: "dev@example.com" } })).status).toBe(400);
    expect((await call(deps, "/instance/users", { token: devToken })).status).toBe(403);
    expect((await call(deps, "/instance/users", { method: "POST", token: devToken, body: { email: "x@example.com" } })).status).toBe(403);
  });

  test("resetting a password replaces the old one", async () => {
    const { deps, token, signIn } = await instance();
    const added = await call(deps, "/instance/users", { method: "POST", token, body: { email: "reset@example.com" } });
    const reset = await call(deps, `/instance/users/${added.json.id}/password`, { method: "POST", token });
    expect(reset.json.password).not.toBe(added.json.password);
    expect((await signIn("reset@example.com", added.json.password)).status).toBe(401);
    expect((await signIn("reset@example.com", reset.json.password)).status).toBe(200);
  });

  test("removing a user signs them out and takes their projects", async () => {
    const { deps, token, signIn, admin } = await instance();
    const added = await call(deps, "/instance/users", { method: "POST", token, body: { email: "gone@example.com" } });
    const devToken = (await signIn("gone@example.com", added.json.password)).json.access_token;
    const me = await call(deps, "/me", { token });
    const project = await call(deps, "/projects", { method: "POST", token, body: { org_id: me.json.OrganizationID, name: `p-${n++}` } });
    await call(deps, `/projects/${project.json.ID}/collaborators`, { method: "POST", token, body: { user_id: added.json.id } });

    expect((await call(deps, `/instance/users/${added.json.id}`, { method: "DELETE", token })).status).toBe(204);
    expect((await call(deps, "/me", { token: devToken })).status).toBe(401);
    expect((await signIn("gone@example.com", added.json.password)).status).toBe(401);
    expect((await call(deps, `/projects/${project.json.ID}/collaborators`, { token })).json).toEqual([]);

    const adminId = (await call(deps, "/me", { token })).json.ID;
    expect((await call(deps, `/instance/users/${adminId}`, { method: "DELETE", token })).json.error).toContain("admin can't be removed");
    expect(admin).toBeString();
  });

  test("everyone changes their own password with the current one", async () => {
    const { deps, token, admin, signIn } = await instance();
    const change = (current_password: string, new_password: string) =>
      call(deps, "/me/password", { method: "POST", token, body: { current_password, new_password } });
    expect((await change("wrong", "a long new password")).status).toBe(403);
    expect((await change("admin password 1", "short")).status).toBe(400);
    expect((await change("admin password 1", "a long new password")).status).toBe(204);
    expect((await signIn(admin, "a long new password")).status).toBe(200);
  });
});

describe("contributors get only the projects they are given", () => {
  async function withContributor() {
    const ctx = await instance();
    const { deps, token, signIn } = ctx;
    const added = await call(deps, "/instance/users", { method: "POST", token, body: { email: `c-${n++}@example.com` } });
    const devToken = (await signIn(added.json.email, added.json.password)).json.access_token as string;
    const me = await call(deps, "/me", { token });
    const project = await call(deps, "/projects", { method: "POST", token, body: { org_id: me.json.OrganizationID, name: `shared-${n++}` } });
    const envs = await call(deps, `/projects/${project.json.ID}/environments`, { token });
    const prod = await call(deps, `/projects/${project.json.ID}/environments`, { method: "POST", token, body: { name: "prod", is_production: true } });
    await call(deps, `/environments/${envs.json[0].ID}/secrets`, { method: "PUT", token, body: { K: "v" } });
    return { ...ctx, devToken, userId: added.json.id as string, project: project.json, env: envs.json[0], prod: prod.json };
  }

  test("a contributor can't create projects; the admin can", async () => {
    const { deps, devToken } = await withContributor();
    const devMe = await call(deps, "/me", { token: devToken });
    const refused = await call(deps, "/projects", { method: "POST", token: devToken, body: { org_id: devMe.json.OrganizationID, name: "mine" } });
    expect(refused.status).toBe(403);
    expect(refused.json.error).toContain("only the admin");
  });

  test("a grant gives a project, and the admin's list shows it", async () => {
    const { deps, token, devToken, userId, project, env, prod } = await withContributor();
    expect((await call(deps, "/projects", { token: devToken })).json).toEqual([]);

    const grant = await call(deps, `/projects/${project.ID}/collaborators`, { method: "POST", token, body: { user_id: userId, permission: "write" } });
    expect(grant.status).toBe(201);
    expect(grant.json).toMatchObject({ project_id: project.ID, project_name: project.Name, permission: "write", environment_id: "" });

    expect((await call(deps, "/projects", { token: devToken })).json).toEqual([project]);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token: devToken })).json).toEqual({ K: "v" });
    expect((await call(deps, `/environments/${prod.ID}/secrets`, { token: devToken })).status).toBe(403);

    const again = await call(deps, `/projects/${project.ID}/collaborators`, { method: "POST", token, body: { user_id: userId, permission: "read" } });
    const users = await call(deps, "/instance/users", { token });
    const dev = users.json.find((u: any) => u.id === userId);
    expect(dev.grants).toEqual([again.json]);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: devToken, body: { K: "x" } })).status).toBe(403);
  });

  test("a grant can be one environment, production included", async () => {
    const { deps, token, devToken, userId, project, env, prod } = await withContributor();
    await call(deps, `/projects/${project.ID}/collaborators`, { method: "POST", token, body: { user_id: userId, environment_id: prod.ID } });
    expect((await call(deps, `/environments/${prod.ID}/secrets`, { token: devToken })).status).toBe(200);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token: devToken })).status).toBe(403);
  });

  test("bad grants are refused", async () => {
    const { deps, token, devToken, userId, project } = await withContributor();
    const grant = (t: string, body: object) => call(deps, `/projects/${project.ID}/collaborators`, { method: "POST", token: t, body });
    expect((await grant(token, { user_id: userId, permission: "root" })).status).toBe(400);
    expect((await grant(token, { user_id: crypto.randomUUID() })).status).toBe(400);
    expect((await grant(token, { user_id: userId, environment_id: crypto.randomUUID() })).status).toBe(400);
    expect((await grant(devToken, { user_id: userId })).status).toBe(403);
    expect((await grant(token, {})).status).toBe(400);
  });

  test("on an email-sign-in server, people are invited by email instead", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const { project } = await newProject(deps, owner);
    const reply = await call(deps, `/projects/${project.ID}/collaborators`, { method: "POST", token: owner.token, body: { user_id: owner.userId } });
    expect(reply.status).toBe(404);
    expect((await call(deps, "/instance/users", { token: owner.token })).status).toBe(404);
  });
});
