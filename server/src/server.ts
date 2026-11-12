// The Bun entry: a long-running server, for local development and self-hosting.

import pg from "pg";
import { app } from "./app";
import { readConfig } from "./config";
import { makeDb, useTypes } from "./db";
import { bunRedis, upstashRedis } from "./redis";
import { billingPass } from "./services/billing";
import { usersFile } from "./services/instance";
import { syncUsers } from "./services/members";
import { makeDeps } from "./setup";

const config = readConfig(process.env);
useTypes(pg.types);
const pool = new pg.Pool({ connectionString: config.databaseUrl });
pool.on("error", (err) => console.error(`database: ${err.message}`)); // a dropped idle connection, not a failed query
const db = makeDb(pool);
await db.query("SELECT 1");
const redis = config.upstashUrl ? upstashRedis(config.upstashUrl, config.upstashToken) : bunRedis(config.redisUrl);
await redis.get("envi:ping");

const users = config.usersFile ? usersFile(config.usersFile) : null;
const deps = await makeDeps(config, db, redis, (work) => void work.catch((err) => console.error(err)), users);
if (users) console.log(`password sign-in: ${(await syncUsers(deps)).length} user(s) in ${config.usersFile}`);
console.log(`mode: ${config.mode} (billing ${deps.bachs ? "on" : "off"}), ENVI_ADMINS: ${config.admins.length} address(es)`);

const server = Bun.serve({ port: config.port, fetch: (request) => app.fetch(request, { deps }) });
console.log(`API listening on :${server.port}`);

const hourly = setInterval(() => billingPass(deps).catch((err) => console.error(`billing pass: ${err.message}`)), 60 * 60 * 1000);
void billingPass(deps).catch((err) => console.error(`billing pass: ${err.message}`));

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, async () => {
    clearInterval(hourly);
    await server.stop();
    await db.end();
    process.exit(0);
  });
}
