import { describe, expect, test } from "bun:test";
import { call, newProject, newUser, testDeps } from "./helpers";

describe("projects and environments", () => {
  test("a new project has one default environment", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project, env } = await newProject(deps, user, "shop");
    expect(project).toEqual({ ID: expect.any(String), OrgID: user.orgId, Name: "shop" });
    expect(env).toEqual({ ID: expect.any(String), ProjectID: project.ID, Name: "default", Production: false });

    const listed = await call(deps, "/projects", { token: user.token });
    expect(listed.json).toEqual([project]);
  });

  test("environments can be added, renamed and deleted", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project } = await newProject(deps, user);
    const prod = await call(deps, `/projects/${project.ID}/environments`, { method: "POST", token: user.token, body: { name: "prod", is_production: true } });
    expect(prod.status).toBe(201);
    expect(prod.json.Production).toBe(true);

    const renamed = await call(deps, `/environments/${prod.json.ID}`, { method: "PATCH", token: user.token, body: { name: "production", is_production: true } });
    expect(renamed.json.Name).toBe("production");

    const names = async () => (await call(deps, `/projects/${project.ID}/environments`, { token: user.token })).json.map((e: any) => e.Name);
    expect(await names()).toEqual(["default", "production"]);
    expect((await call(deps, `/environments/${prod.json.ID}`, { method: "DELETE", token: user.token })).status).toBe(204);
    expect(await names()).toEqual(["default"]);
  });

  test("duplicate names conflict", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project } = await newProject(deps, user, "dup");
    expect((await call(deps, "/projects", { method: "POST", token: user.token, body: { org_id: user.orgId, name: "dup" } })).status).toBe(409);
    expect((await call(deps, `/projects/${project.ID}/environments`, { method: "POST", token: user.token, body: { name: "default" } })).status).toBe(409);
  });

  test("strangers cannot see or change a project", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const stranger = await newUser(deps);
    const { project, env } = await newProject(deps, owner);
    expect((await call(deps, "/projects", { method: "POST", token: stranger.token, body: { org_id: owner.orgId, name: "x" } })).status).toBe(403);
    expect((await call(deps, `/projects/${project.ID}/environments`, { token: stranger.token })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}`, { method: "PATCH", token: stranger.token, body: { name: "x" } })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}`, { method: "DELETE", token: stranger.token })).status).toBe(403);
    expect((await call(deps, `/projects/${project.ID}`, { method: "DELETE", token: stranger.token })).status).toBe(403);
    expect((await call(deps, "/projects", { token: stranger.token })).json).toEqual([]);
  });

  test("deleting a project removes it", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project } = await newProject(deps, user);
    expect((await call(deps, `/projects/${project.ID}`, { method: "DELETE", token: user.token })).status).toBe(204);
    expect((await call(deps, "/projects", { token: user.token })).json).toEqual([]);
  });
});

describe("secrets", () => {
  test("values are written, read back and deleted", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { env } = await newProject(deps, user);
    const path = `/environments/${env.ID}/secrets`;

    expect((await call(deps, path, { method: "PUT", token: user.token, body: { API_KEY: "abc", DB_URL: "postgres://x" } })).status).toBe(204);
    expect((await call(deps, path, { token: user.token })).json).toEqual({ API_KEY: "abc", DB_URL: "postgres://x" });

    expect((await call(deps, `${path}/API_KEY`, { method: "DELETE", token: user.token })).status).toBe(204);
    expect((await call(deps, path, { token: user.token })).json).toEqual({ DB_URL: "postgres://x" });
    expect((await call(deps, `${path}/API_KEY`, { method: "DELETE", token: user.token })).status).toBe(404);
  });

  test("values are stored encrypted", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { env } = await newProject(deps, user);
    await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: user.token, body: { PLAIN: "visible-text" } });
    const rows = await deps.db.query(
      `SELECT v.ciphertext FROM secret_versions v JOIN secrets s ON s.id = v.secret_id WHERE s.environment_id = $1`, [env.ID],
    );
    expect(rows.length).toBeGreaterThan(0);
    for (const row of rows) expect(Buffer.from(row.ciphertext).toString("latin1")).not.toContain("visible-text");
  });

  test("snapshot writes refuse a stale revision", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { env } = await newProject(deps, user);
    const path = `/environments/${env.ID}/secrets/snapshot`;

    const empty = await call(deps, path, { token: user.token });
    expect(empty.json).toEqual({ values: {}, revision: 0 });

    const first = await call(deps, path, { method: "PUT", token: user.token, body: { values: { A: "1", B: "2" }, expected_revision: 0 } });
    expect(first.status).toBe(200);
    expect(first.json.revision).toBeGreaterThan(0);

    const stale = await call(deps, path, { method: "PUT", token: user.token, body: { values: { A: "x" }, expected_revision: 0 } });
    expect(stale.status).toBe(409);
    expect(stale.json.code).toBe("stale_revision");

    const second = await call(deps, path, { method: "PUT", token: user.token, body: { values: { A: "1" }, expected_revision: first.json.revision } });
    expect(second.status).toBe(200);
    expect((await call(deps, path, { token: user.token })).json).toEqual({ values: { A: "1", B: "2" }, revision: second.json.revision });
  });

  test("bad bodies are a 400", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { env } = await newProject(deps, user);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: user.token, body: { A: 1 } })).status).toBe(400);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: user.token, body: "nope" })).status).toBe(400);
    expect((await call(deps, `/environments/${env.ID}/secrets/snapshot`, { method: "PUT", token: user.token, body: { expected_revision: 0 } })).status).toBe(400);
  });

  test("strangers cannot read or write", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const stranger = await newUser(deps);
    const { env } = await newProject(deps, owner);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token: stranger.token })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: stranger.token, body: { A: "1" } })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}/secrets/snapshot`, { method: "PUT", token: stranger.token, body: { values: {}, expected_revision: 0 } })).status).toBe(403);
  });
});

describe("values by name", () => {
  test("a person names the project and environment", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { env } = await newProject(deps, user, "api");
    await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: user.token, body: { K: "v" } });

    const values = await call(deps, "/values?project=api", { token: user.token });
    expect(values.json).toEqual({ project: "api", environment: "default", values: { K: "v" }, revision: expect.any(Number) });

    const missing = await call(deps, "/values?project=apj", { token: user.token });
    expect(missing.status).toBe(404);
    expect(missing.json.code).toBe("project_not_found");
    expect(missing.json.error).toContain("api");

    expect((await call(deps, "/values?project=api&environment=prod", { token: user.token })).json.code).toBe("environment_not_found");
    expect((await call(deps, "/values", { token: user.token })).status).toBe(400);
  });

  test("several environments need a name", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project } = await newProject(deps, user, "multi");
    await call(deps, `/projects/${project.ID}/environments`, { method: "POST", token: user.token, body: { name: "prod" } });
    const reply = await call(deps, "/values?project=multi", { token: user.token });
    expect(reply.status).toBe(404);
    expect(reply.json.error).toContain("default, prod");
    expect((await call(deps, "/values?project=multi&environment=prod", { token: user.token })).status).toBe(200);
  });
});

describe("service tokens", () => {
  async function setup(permission: string) {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project, env } = await newProject(deps, user, "svc");
    await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token: user.token, body: { K: "v" } });
    const created = await call(deps, `/projects/${project.ID}/environments/${env.ID}/service-tokens`, {
      method: "POST", token: user.token, body: { name: "ci", permission },
    });
    expect(created.status).toBe(201);
    return { deps, user, project, env, token: created.json.Value as string, created: created.json };
  }

  test("a read token reads only its own environment", async () => {
    const { deps, user, project, env, token, created } = await setup("read");
    expect(created).toEqual({ ID: expect.any(String), Value: expect.any(String), Permission: "read", ExpiresAt: null });

    expect((await call(deps, `/environments/${env.ID}/secrets`, { token })).json).toEqual({ K: "v" });
    expect((await call(deps, `/environments/${env.ID}/secrets/snapshot`, { token })).json.values).toEqual({ K: "v" });
    expect((await call(deps, "/values", { token })).json).toMatchObject({ project: "svc", environment: "default", values: { K: "v" } });
    expect((await call(deps, "/values?project=other", { token })).status).toBe(403);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token, body: { K: "x" } })).status).toBe(403);

    const other = await call(deps, `/projects/${project.ID}/environments`, { method: "POST", token: user.token, body: { name: "prod" } });
    expect((await call(deps, `/environments/${other.json.ID}/secrets`, { token })).status).toBe(403);
    expect((await call(deps, "/projects", { method: "POST", token, body: { org_id: user.orgId, name: "x" } })).status).toBe(403);
  });

  test("a write token writes, and records no person as author", async () => {
    const { deps, env, token } = await setup("write");
    expect((await call(deps, `/environments/${env.ID}/secrets`, { method: "PUT", token, body: { NEW: "1" } })).status).toBe(204);
    const snapshot = await call(deps, `/environments/${env.ID}/secrets/snapshot`, { token });
    const put = await call(deps, `/environments/${env.ID}/secrets/snapshot`, {
      method: "PUT", token, body: { values: { ONLY: "1" }, expected_revision: snapshot.json.revision },
    });
    expect(put.status).toBe(200);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token })).json).toEqual({ K: "v", NEW: "1", ONLY: "1" });
  });

  test("a revoked token stops working", async () => {
    const { deps, user, env, token } = await setup("read");
    expect((await call(deps, "/service-tokens", { method: "DELETE", token: user.token, headers: { "X-Service-Token": token } })).status).toBe(204);
    expect((await call(deps, `/environments/${env.ID}/secrets`, { token })).status).toBe(401);
    expect((await call(deps, "/service-tokens", { method: "DELETE", token: user.token, headers: { "X-Service-Token": token + "x" } })).status).toBe(404);
  });

  test("only an owner or admin may create one", async () => {
    const deps = await testDeps();
    const owner = await newUser(deps);
    const stranger = await newUser(deps);
    const { project, env } = await newProject(deps, owner);
    const reply = await call(deps, `/projects/${project.ID}/environments/${env.ID}/service-tokens`, {
      method: "POST", token: stranger.token, body: { name: "x" },
    });
    expect(reply.status).toBe(403);
  });
});

describe("key-value store", () => {
  test("entries are set, listed and deleted", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    await newProject(deps, user, "kvp");
    expect((await call(deps, "/kv", { method: "PUT", token: user.token, body: { project: "kvp", key: "flag", value: "on" } })).json).toEqual({ project: "kvp", key: "flag" });
    expect((await call(deps, "/kv?project=kvp", { token: user.token })).json).toEqual({ project: "kvp", values: { flag: "on" } });
    expect((await call(deps, "/kv", { method: "DELETE", token: user.token, body: { project: "kvp", key: "flag" } })).status).toBe(204);
    expect((await call(deps, "/kv?project=kvp", { token: user.token })).json.values).toEqual({});
  });

  test("bad keys and large values are refused", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    await newProject(deps, user, "kvbad");
    expect((await call(deps, "/kv", { method: "PUT", token: user.token, body: { project: "kvbad", key: "a\nb", value: "" } })).status).toBe(400);
    expect((await call(deps, "/kv", { method: "PUT", token: user.token, body: { project: "kvbad", key: "big", value: "x".repeat(64 * 1024 + 1) } })).status).toBe(400);
    expect((await call(deps, "/kv", { method: "PUT", token: user.token, body: { project: "kvbad", value: "x" } })).status).toBe(400);
    expect((await call(deps, "/kv?project=nope", { token: user.token })).status).toBe(404);
  });

  test("a read service token may read but not write", async () => {
    const deps = await testDeps();
    const user = await newUser(deps);
    const { project, env } = await newProject(deps, user, "kvsvc");
    await call(deps, "/kv", { method: "PUT", token: user.token, body: { project: "kvsvc", key: "k", value: "v" } });
    const created = await call(deps, `/projects/${project.ID}/environments/${env.ID}/service-tokens`, { method: "POST", token: user.token, body: { name: "r" } });
    const token = created.json.Value;
    expect((await call(deps, "/kv", { token })).json).toEqual({ project: "kvsvc", values: { k: "v" } });
    expect((await call(deps, "/kv?project=elsewhere", { token })).status).toBe(403);
    expect((await call(deps, "/kv", { method: "PUT", token, body: { key: "k", value: "x" } })).status).toBe(403);
  });
});
