import { isLocalUrl, type Config } from "./config";
import { makeCipher } from "./crypto";
import type { Db } from "./db";
import type { Deps } from "./deps";
import { noMailer, resendMailer } from "./mail";
import type { Redis } from "./redis";
import { bachsClient, type Bachs } from "./services/bachs";
import type { Users } from "./services/instance";

// makeDeps assembles everything a request needs from the config and the runtime's db and redis.
export async function makeDeps(
  config: Config, db: Db, redis: Redis, background: (work: Promise<unknown>) => void, users: Users | null = null,
): Promise<Deps> {
  return {
    config,
    db,
    redis,
    cipher: await makeCipher(config.encryptionKey),
    // A self-hosted instance with a users file needs no email provider.
    mailer: config.resendApiKey ? resendMailer(config.resendApiKey, config.resendFrom) : noMailer,
    bachs: billingProvider(config),
    users,
    background,
    now: () => new Date(),
  };
}

// billingProvider returns the Bachs client when this deployment bills, or null.
export function billingProvider(config: Config): Bachs | null {
  if (config.mode !== "hosted") return null;
  let client: Bachs;
  try {
    client = bachsClient(config.bachs.secretKey);
  } catch (err) {
    console.warn(`WARNING: billing is OFF: ${(err as Error).message} (hosted mode needs BACHS_SECRET_KEY)`);
    return null;
  }
  // A live key with a local database or dashboard is a laptop, whatever ENVIRONMENT says.
  if (client.live && (!config.environment || isLocalUrl(config.databaseUrl) || isLocalUrl(config.webUrl))) {
    throw new Error("refusing to start: a live Bachs key (sk_live_) on a local instance; use an sk_sandbox_ key locally");
  }
  if (!client.live && config.environment === "production") console.warn("WARNING: production is using a sandbox Bachs key — no real money moves");
  if (!config.bachs.webhookSecret) console.warn("WARNING: BACHS_WEBHOOK_SECRET is unset — payments will be taken but never recorded");
  return client;
}
