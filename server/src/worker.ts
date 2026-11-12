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
  if (env.NEON_LOCAL_PROXY) useLocalProxy(env.NEON_LOCAL_PROXY);
  const config = readConfig(env);
  const pool = new Pool({ connectionString: config.databaseUrl });
  pool.on("error", (err: Error) => console.error(`database: ${err.message}`)); // a dropped idle connection, not a failed query
  const db = makeDb(pool);
  const redis = upstashRedis(config.upstashUrl, config.upstashToken);
  const deps = await makeDeps(config, db, redis, (work) => ctx.waitUntil(work.catch((err) => console.error(err))));
  return { deps, db };
}

// useLocalProxy points the driver at a plain WebSocket proxy (test/worker/compose.yml) instead of Neon.
function useLocalProxy(address: string) {
  neonConfig.wsProxy = () => `${address}/v1`;
  neonConfig.useSecureWebSocket = false;
  neonConfig.pipelineTLS = false;
  neonConfig.pipelineConnect = false;
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
      await db.end();
    }
  },
};
