export type Mode = "self-hosted" | "hosted";

export type Config = {
  databaseUrl: string;
  redisUrl: string;
  upstashUrl: string;
  upstashToken: string;
  encryptionKey: string;
  resendApiKey: string;
  resendFrom: string;
  port: number;
  host: string;
  webUrl: string;
  docsUrl: string;
  environment: string;
  admins: string[];
  mode: Mode;
  // usersFile turns on password sign-in with the users kept in this file (a self-hosted instance).
  usersFile: string;
  currency: "NGN" | "USD";
  bachs: { secretKey: string; webhookSecret: string; teamProductId: string; returnUrl: string };
};

type Env = Record<string, string | undefined>;

// setting reads a variable, dropping surrounding spaces and one pair of matching quotes.
export function setting(env: Env, name: string): string {
  let value = (env[name] ?? "").trim();
  const quote = value[0];
  if (value.length >= 2 && (quote === '"' || quote === "'") && value.at(-1) === quote) {
    value = value.slice(1, -1).trim();
  }
  return value;
}

export function loadConfig(env: Env): Config {
  const webUrl = setting(env, "ENVI_WEB_URL") || "http://localhost:3000";
  const mode = setting(env, "ENVI_MODE").toLowerCase() === "hosted" ? "hosted" : "self-hosted";
  const currency = setting(env, "ENVI_CURRENCY").toUpperCase() === "USD" ? "USD" : "NGN";
  return {
    databaseUrl: setting(env, "DATABASE_URL"),
    redisUrl: setting(env, "REDIS_URL"),
    upstashUrl: setting(env, "UPSTASH_REDIS_REST_URL"),
    upstashToken: setting(env, "UPSTASH_REDIS_REST_TOKEN"),
    encryptionKey: env.ENVI_ENCRYPTION_KEY ?? "",
    resendApiKey: setting(env, "RESEND_API_KEY"),
    resendFrom: setting(env, "RESEND_FROM"),
    port: Number(setting(env, "ENVI_API_PORT") || 8080),
    host: setting(env, "ENVI_API_HOST") || "0.0.0.0",
    webUrl,
    docsUrl: setting(env, "ENVI_DOCS_URL") || defaultDocsUrl(webUrl),
    environment: setting(env, "ENVIRONMENT"),
    admins: setting(env, "ENVI_ADMINS").split(",").map(normaliseEmail).filter(Boolean),
    mode,
    usersFile: setting(env, "ENVI_USERS_FILE"),
    currency,
    bachs: {
      secretKey: setting(env, "BACHS_SECRET_KEY"),
      webhookSecret: setting(env, "BACHS_WEBHOOK_SECRET"),
      teamProductId: setting(env, "BACHS_TEAM_PRODUCT_ID"),
      returnUrl: setting(env, "BACHS_RETURN_URL"),
    },
  };
}

// readConfig loads the config and throws the first problem that stops the server starting.
export function readConfig(env: Env): Config {
  const c = loadConfig(env);
  if (!c.databaseUrl) throw new Error("DATABASE_URL is required");
  if (!c.redisUrl && !(c.upstashUrl && c.upstashToken)) {
    throw new Error("REDIS_URL (or UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN) is required");
  }
  if (!c.usersFile && (!c.resendApiKey || !c.resendFrom)) throw new Error("RESEND_API_KEY and RESEND_FROM are required");
  if (new TextEncoder().encode(c.encryptionKey).length !== 32) {
    throw new Error("ENVI_ENCRYPTION_KEY must be exactly 32 bytes");
  }
  if (isDeployed(c) && isLocalUrl(c.webUrl)) {
    throw new Error(
      `ENVI_WEB_URL must be set to the public dashboard URL when ENVIRONMENT is set (got ${c.webUrl}); invitation and device-approval links are built from it`,
    );
  }
  return c;
}

export function isDeployed(c: Config): boolean {
  return !["", "development", "dev", "local", "test"].includes(c.environment.trim().toLowerCase());
}

// isLocalUrl reports an address only this machine can reach.
export function isLocalUrl(raw: string): boolean {
  let url: URL;
  try {
    url = new URL(raw.trim());
  } catch {
    return false;
  }
  const host = url.hostname.replace(/^\[|\]$/g, "");
  if (host === "") return url.protocol === "postgres:" || url.protocol === "postgresql:";
  if (host === "localhost" || host.endsWith(".localhost")) return true;
  return host === "::1" || host === "::" || host === "0.0.0.0" || host.startsWith("127.");
}

export function normaliseEmail(email: string): string {
  return email.trim().toLowerCase();
}

function defaultDocsUrl(webUrl: string): string {
  if (webUrl.includes("envisecrets.com")) return "https://docs.envisecrets.com";
  return webUrl.replace(/\/+$/, "") + "/docs";
}
