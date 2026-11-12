// The Cloudflare Workers entry. Workers can't keep connections between requests, so each
// request (and each cron run) opens its own Neon pool and closes it afterwards.

import { neonConfig, Pool, types } from "@neondatabase/serverless";
import { app } from "./app";
import { readConfig } from "./config";
import { makeDb, useTypes } from "./db";
import { upstashRedis } from "./redis";
import { billingPass } from "./services/billing";
import { makeDeps } from "./setup";

useTypes(types);
neonConfig.poolQueryViaFetch = false;

type Env = Record<string, string>;

async function depsFor(env: Env, ctx: ExecutionContext) {
  const config = readConfig(env);
  const db = makeDb(new Pool({ connectionString: config.databaseUrl }));
  const redis = upstashRedis(config.upstashUrl, config.upstashToken);
  const deps = await makeDeps(config, db, redis, (work) => ctx.waitUntil(work.catch((err) => console.error(err))));
  return { deps, db };
}

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const { deps, db } = await depsFor(env, ctx);
    const response = await app.fetch(request, { deps }, ctx);
    ctx.waitUntil(db.end());
    return response;
  },

  // Runs hourly (see wrangler.toml): counts people, closes finished months, retries failed charges.
  async scheduled(_event: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void> {
    const { deps, db } = await depsFor(env, ctx);
    try {
      await billingPass(deps);
    } finally {
      ctx.waitUntil(db.end());
    }
  },
};
