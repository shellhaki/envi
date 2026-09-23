import { Hono } from "hono";
import { Envi, EnviAuthError, EnviMissingKeyError, EnviUnreachableError } from "@shellhaki/envi-sdk";

const token = process.env.ENVI_TOKEN;
if (!token) {
  console.error("ENVI_TOKEN is required. Create one with: envi token create --name sdk-demo --permission write");
  process.exit(1);
}

const envi = new Envi({ token, baseUrl: process.env.ENVI_API_URL });

const app = new Hono();

app.onError((error, c) => {
  if (error instanceof EnviMissingKeyError) return c.json({ error: error.message }, 404);
  if (error instanceof EnviAuthError) return c.json({ error: error.message }, 403);
  if (error instanceof EnviUnreachableError) return c.json({ error: error.message }, 503);
  return c.json({ error: String(error) }, 500);
});

app.get("/", async (c) => c.json({ project: envi.project, values: await envi.all() }));

app.get("/refresh", async (c) => {
  await envi.refresh();
  return c.json({ values: await envi.all() });
});

app.get("/:key", async (c) => {
  const key = c.req.param("key");
  const value = await envi.get(key);
  if (value === undefined) return c.json({ key, error: "not set" }, 404);
  return c.json({ key, value });
});

app.put("/:key", async (c) => {
  const key = c.req.param("key");
  const value = await c.req.text();
  await envi.set(key, value);
  return c.json({ key, value });
});

app.delete("/:key", async (c) => {
  const key = c.req.param("key");
  await envi.delete(key);
  return c.json({ key, deleted: true });
});

// Load the store once at startup so a bad credential fails here, not mid-request.
try {
  await envi.all();
} catch (error) {
  console.error("Could not reach Envi:", (error as Error).message);
  process.exit(1);
}

console.log(`listening on http://localhost:3000 — project ${envi.project}`);

export default app;
